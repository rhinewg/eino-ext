/*
 * Copyright 2024 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package pdf

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/components/document/parser"
	"github.com/stretchr/testify/assert"
)

// TestPDFParser_TextMode 测试纯文本提取模式
func TestPDFParser_TextMode(t *testing.T) {
	t.Run("ExtractText_Simple", func(t *testing.T) {
		ctx := context.Background()

		f, err := os.Open("./testdata/test_pdf.pdf")
		assert.NoError(t, err)
		defer f.Close()

		p, err := NewPDFParser(ctx, nil)
		assert.NoError(t, err)

		docs, err := p.Parse(ctx, f)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(docs)) // 默认合并为 1 个文档
		assert.True(t, len(docs[0].Content) > 0)
	})

	t.Run("ExtractText_ByPages", func(t *testing.T) {
		ctx := context.Background()

		f, err := os.Open("./testdata/test_pdf.pdf")
		assert.NoError(t, err)
		defer f.Close()

		p, err := NewPDFParser(ctx, &Config{ToPages: true})
		assert.NoError(t, err)

		docs, err := p.Parse(ctx, f, WithToPages(true))
		assert.NoError(t, err)
		assert.True(t, len(docs) >= 1)
		assert.True(t, len(docs[0].Content) > 0)
	})
}

// TestParsePDFPageRange 测试页范围解析
func TestParsePDFPageRange(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectFirst int
		expectLast  int
		expectError bool
	}{
		{"SinglePage", "5", 5, 5, false},
		{"Range", "1-10", 1, 10, false},
		{"FromPageToEnd", "3-", 3, 0, false},
		{"FromStartToPage", "-5", 0, 5, false},
		{"EmptyString", "", 0, 0, true},
		{"InvalidNumber", "abc", 0, 0, true},
		{"InvalidRange", "abc-def", 0, 0, true},
		{"NegativePage", "-1", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := ParsePDFPageRange(tt.input)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expectFirst, opts.FirstPage)
			assert.Equal(t, tt.expectLast, opts.LastPage)
		})
	}
}

// TestPDFParser_ImageMode 测试图片提取模式
func TestPDFParser_ImageMode(t *testing.T) {
	// 跳过 pdftoppm 未安装的情况
	if !isPDFToppmAvailable() {
		t.Skip("pdftoppm not installed, skipping image mode test")
	}

	ctx := context.Background()

	f, err := os.Open("./testdata/test_pdf.pdf")
	assert.NoError(t, err)
	defer f.Close()

	p, err := NewPDFParser(ctx, &Config{
		ExtractAsImages: true,
		DPI:             72, // 使用较低 DPI 加快测试速度
	})
	assert.NoError(t, err)

	docs, err := p.Parse(ctx, f, WithExtractAsImages(true))
	assert.NoError(t, err)
	assert.True(t, len(docs) >= 1)

	// 验证返回的文档包含图片元数据
	for _, doc := range docs {
		assert.Contains(t, doc.MetaData, "page_number")
		assert.Contains(t, doc.MetaData, "image_base64")
		assert.Contains(t, doc.MetaData, "content_type")
		assert.Equal(t, "image/jpeg", doc.MetaData["content_type"])
	}
}

// TestPDFParser_PageRange 测试按页范围提取
func TestPDFParser_PageRange(t *testing.T) {
	if !isPDFToppmAvailable() {
		t.Skip("pdftoppm not installed, skipping page range test")
	}

	ctx := context.Background()

	f, err := os.Open("./testdata/test_pdf.pdf")
	assert.NoError(t, err)
	defer f.Close()

	p, err := NewPDFParser(ctx, nil)
	assert.NoError(t, err)

	// 提取第 1 页
	docs, err := p.Parse(ctx, f, WithPageRange("1"))
	assert.NoError(t, err)
	assert.Equal(t, 1, len(docs))
	assert.Equal(t, 1, docs[0].MetaData["page_number"])
}

// TestPDFParser_LargeFile 测试大文件自动降级为图片模式
func TestPDFParser_LargeFile(t *testing.T) {
	if !isPDFToppmAvailable() {
		t.Skip("pdftoppm not installed, skipping large file test")
	}

	ctx := context.Background()

	// 创建一个临时"大" PDF 文件（模拟）
	// 注意：实际测试中应该使用真实的大 PDF 文件
	// 这里仅测试配置逻辑
	p, err := NewPDFParser(ctx, &Config{
		MaxExtractSize: 1024, // 设置为很小的值，模拟大文件
	})
	assert.NoError(t, err)

	f, err := os.Open("./testdata/test_pdf.pdf")
	assert.NoError(t, err)
	defer f.Close()

	// 由于 MaxExtractSize 设置很小，应该自动触发图片模式
	docs, err := p.Parse(ctx, f)
	// 如果 pdftoppm 可用，应返回图片模式的结果
	if isPDFToppmAvailable() {
		assert.NoError(t, err)
		if len(docs) > 0 {
			assert.Contains(t, docs[0].MetaData, "image_base64")
		}
	}
}

// TestRenderPool 测试渲染池
func TestRenderPool(t *testing.T) {
	pool := NewPDFRenderPool(PoolConfig{
		MaxConcurrent: 2,
		QueueTimeout:  5,
	})

	stats := pool.GetStats()
	assert.Equal(t, 2, stats.MaxConcurrent)
	assert.Equal(t, 0, stats.ActiveTasks)
	assert.Equal(t, 2, stats.Available)
}

// TestPDFValidation 测试 PDF 文件验证
func TestPDFValidation(t *testing.T) {
	t.Run("EmptyFile", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "empty-*.pdf")
		assert.NoError(t, err)
		tmpPath := tmpFile.Name()
		tmpFile.Close()
		defer os.Remove(tmpPath)

		err = validatePDFHeader(tmpPath)
		assert.Error(t, err) // 空文件无法读取 header
	})

	t.Run("NonPDFFile", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "notpdf-*.txt")
		assert.NoError(t, err)
		tmpPath := tmpFile.Name()
		_, _ = tmpFile.WriteString("This is not a PDF file")
		tmpFile.Close()
		defer os.Remove(tmpPath)

		err = validatePDFHeader(tmpPath)
		assert.Error(t, err, ErrPDFCorrupted)
	})

	t.Run("ValidPDFFile", func(t *testing.T) {
		err := validatePDFHeader("./testdata/test_pdf.pdf")
		assert.NoError(t, err)
	})
}

// TestGetPDFPageCount 测试获取页数
func TestGetPDFPageCount(t *testing.T) {
	ctx := context.Background()

	if !isPDFToppmAvailable() {
		t.Skip("pdftoppm not installed, using fallback")
	}

	count, err := GetPDFPageCount(ctx, "./testdata/test_pdf.pdf")
	assert.NoError(t, err)
	assert.True(t, count >= 1)
}

// TestPDFToppmAvailability 测试 pdftoppm 可用性
func TestPDFToppmAvailability(t *testing.T) {
	// 这个测试只是验证函数不会 panic
	available := isPDFToppmAvailable()
	t.Logf("pdftoppm available: %v", available)
}

// TestPDFError 测试错误类型
func TestPDFError(t *testing.T) {
	err := ErrPDFEmpty
	assert.Equal(t, "empty", err.Reason)
	assert.Contains(t, err.Error(), "empty")

	err = ErrPDFTooLarge
	assert.Equal(t, "too_large", err.Reason)
	assert.Contains(t, err.Error(), "too_large")

	err = ErrPDFPasswordProtected
	assert.Equal(t, "password_protected", err.Reason)

	err = ErrPDFToppmUnavailable
	assert.Equal(t, "unavailable", err.Reason)
}

// TestParserOptions 测试解析器选项
func TestParserOptions(t *testing.T) {
	ctx := context.Background()

	t.Run("WithExtractAsImages", func(t *testing.T) {
		p, err := NewPDFParser(ctx, nil)
		assert.NoError(t, err)

		// 验证选项可以正常应用（不实际执行）
		f, err := os.Open("./testdata/test_pdf.pdf")
		assert.NoError(t, err)
		defer f.Close()

		_, _ = p.Parse(ctx, f, WithExtractAsImages(true))
	})

	t.Run("WithDPI", func(t *testing.T) {
		p, err := NewPDFParser(ctx, nil)
		assert.NoError(t, err)

		f, err := os.Open("./testdata/test_pdf.pdf")
		assert.NoError(t, err)
		defer f.Close()

		_, _ = p.Parse(ctx, f, WithDPI(150))
	})

	t.Run("WithPageRange", func(t *testing.T) {
		p, err := NewPDFParser(ctx, nil)
		assert.NoError(t, err)

		f, err := os.Open("./testdata/test_pdf.pdf")
		assert.NoError(t, err)
		defer f.Close()

		_, _ = p.Parse(ctx, f, WithPageRange("1-2"))
	})

	t.Run("MultipleOptions", func(t *testing.T) {
		p, err := NewPDFParser(ctx, nil)
		assert.NoError(t, err)

		f, err := os.Open("./testdata/test_pdf.pdf")
		assert.NoError(t, err)
		defer f.Close()

		_, _ = p.Parse(ctx, f,
			WithExtractAsImages(true),
			WithDPI(100),
			WithPageRange("1"),
		)
	})
}

// BenchmarkPDFParser 性能基准测试
func BenchmarkPDFParser_TextMode(b *testing.B) {
	ctx := context.Background()
	p, _ := NewPDFParser(ctx, nil)

	data, _ := os.ReadFile("./testdata/test_pdf.pdf")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := &fileReader{data: data}
		_, _ = p.Parse(ctx, reader)
	}
}

// fileReader 简单的 io.Reader 实现
type fileReader struct {
	data   []byte
	offset int
}

func (r *fileReader) Read(p []byte) (n int, err error) {
	if r.offset >= len(r.data) {
		return 0, nil
	}
	n = copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}

// TestRenderPoolConcurrent 测试渲染池并发
func TestRenderPoolConcurrent(t *testing.T) {
	if !isPDFToppmAvailable() {
		t.Skip("pdftoppm not installed, skipping concurrent test")
	}

	pool := NewPDFRenderPool(PoolConfig{
		MaxConcurrent: 3,
		QueueTimeout:  10,
	})

	ctx := context.Background()
	pdfPath, _ := filepath.Abs("./testdata/test_pdf.pdf")

	// 并发执行多个任务
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func() {
			_, err := pool.ExecuteWithImages(ctx, pdfPath, &ExtractPDFOptions{
				FirstPage: 1,
				LastPage:  1,
				DPI:       72,
			})
			// 可能成功或因页范围超出而失败，都不应 panic
			if err != nil {
				t.Logf("Expected possible error: %v", err)
			}
			done <- true
		}()
	}

	// 等待所有任务完成
	for i := 0; i < 5; i++ {
		<-done
	}

	stats := pool.GetStats()
	assert.Equal(t, int64(5), stats.TotalCompleted)
}
