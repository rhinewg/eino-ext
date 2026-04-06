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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/schema"
	"github.com/dslipak/pdf"
)

// 智能降级相关常量
const (
	// PDFExtractSizeThreshold 超过此大小的 PDF 将自动转为图片处理 (3MB)
	PDFExtractSizeThreshold = 3 * 1024 * 1024
)

// Config is the configuration for PDF parser.
type Config struct {
	ToPages          bool   // 是否按页拆分文档（文本模式）
	ExtractAsImages  bool   // 是否将 PDF 转为图片（多模态模式）
	DPI              float64 // 图片渲染 DPI，默认 100
	MaxExtractSize   int64  // 超过此大小强制转图片，0 表示使用默认阈值
}

// PDFParser reads from io.Reader and parse its content as plain text or images.
// 支持两种模式：
// 1. 文本模式：提取纯文本（默认）
// 2. 图片模式：将 PDF 页面渲染为 JPEG 图片（适用于多模态模型）
//
// 智能降级策略：
// - 小文件 + 文本模式 → 提取纯文本
// - 大文件 或 图片模式 → 渲染为 JPEG 图片
type PDFParser struct {
	config Config
}

// NewPDFParser creates a new PDF parser.
func NewPDFParser(ctx context.Context, config *Config) (*PDFParser, error) {
	if config == nil {
		config = &Config{}
	}
	if config.DPI <= 0 {
		config.DPI = PDFDefaultDPI
	}
	if config.MaxExtractSize <= 0 {
		config.MaxExtractSize = PDFExtractSizeThreshold
	}
	return &PDFParser{config: *config}, nil
}

// Parse parses the PDF content from io.Reader.
// 根据配置和文件大小自动选择最佳解析策略
func (pp *PDFParser) Parse(ctx context.Context, reader io.Reader, opts ...parser.Option) (docs []*schema.Document, err error) {
	commonOpts := parser.GetCommonOptions(nil, opts...)

	specificOpts := parser.GetImplSpecificOptions(&options{}, opts...)

	// 读取全部数据
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("pdf parser read all from reader failed: %w", err)
	}

	// 检查是否需要转为图片处理
	shouldExtractImages := specificOpts.extractAsImages != nil && *specificOpts.extractAsImages
	if int64(len(data)) > pp.config.MaxExtractSize {
		shouldExtractImages = true
	}

	// 如果指定了页范围，也必须走图片提取路径
	hasPageRange := specificOpts.pageRange != nil && *specificOpts.pageRange != ""

	if shouldExtractImages || hasPageRange {
		return pp.parseAsImages(ctx, data, commonOpts, specificOpts)
	}

	// 默认：提取纯文本
	return pp.parseAsText(ctx, data, commonOpts, specificOpts)
}

// parseAsText 提取纯文本（原有逻辑）
func (pp *PDFParser) parseAsText(ctx context.Context, data []byte, commonOpts *parser.Options, specificOpts *options) ([]*schema.Document, error) {
	readerAt := bytes.NewReader(data)

	f, err := pdf.NewReader(readerAt, int64(readerAt.Len()))
	if err != nil {
		return nil, fmt.Errorf("create new pdf reader failed: %w", err)
	}

	pages := f.NumPage()
	var (
		buf     bytes.Buffer
		toPages = specificOpts.toPages != nil && *specificOpts.toPages
		docs    []*schema.Document
	)
	fonts := make(map[string]*pdf.Font)
	for i := 1; i <= pages; i++ {
		p := f.Page(i)
		for _, name := range p.Fonts() { // cache fonts so we don't continually parse charmap
			if _, ok := fonts[name]; !ok {
				font := p.Font(name)
				fonts[name] = &font
			}
		}
		text, err := p.GetPlainText(fonts)
		if err != nil {
			return nil, fmt.Errorf("read pdf page failed: %w, page= %d", err, i)
		}

		if toPages {
			docs = append(docs, &schema.Document{
				Content:  text,
				MetaData: commonOpts.ExtraMeta,
			})
		} else {
			buf.WriteString(text + "\n")
		}
	}

	if !toPages {
		docs = append(docs, &schema.Document{
			Content:  buf.String(),
			MetaData: commonOpts.ExtraMeta,
		})
	}

	return docs, nil
}

// parseAsImages 将 PDF 页面渲染为图片并返回文档
func (pp *PDFParser) parseAsImages(ctx context.Context, data []byte, commonOpts *parser.Options, specificOpts *options) ([]*schema.Document, error) {
	// 需要将数据写入临时文件，因为 pdftoppm 需要文件路径
	tmpFile, err := os.CreateTemp("", "pdf-parse-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	// 构建提取选项
	extractOpts := &ExtractPDFOptions{
		DPI: pp.config.DPI,
	}

	// 处理页范围
	if specificOpts.pageRange != nil && *specificOpts.pageRange != "" {
		pageRangeOpts, err := ParsePDFPageRange(*specificOpts.pageRange)
		if err != nil {
			return nil, fmt.Errorf("invalid page range '%s': %w", *specificOpts.pageRange, err)
		}
		extractOpts.FirstPage = pageRangeOpts.FirstPage
		extractOpts.LastPage = pageRangeOpts.LastPage
	}

	// 使用渲染池执行，控制并发
	images, err := GetDefaultPool().ExecuteWithImages(ctx, tmpPath, extractOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to extract PDF pages as images: %w", err)
	}

	// 将图片数据编码为 base64 并封装为 Document
	docs := make([]*schema.Document, 0, len(images))
	for _, img := range images {
		doc := &schema.Document{
			Content: fmt.Sprintf("[Page %d] [Image: %d bytes]", img.PageNumber, img.FileSize),
			MetaData: map[string]any{
				"page_number":    img.PageNumber,
				"image_base64":   img.JPEGData, // 原始 JPEG 数据，供上层使用
				"image_size":     img.FileSize,
				"content_type":   "image/jpeg",
				"_source":        commonOpts.URI,
			},
		}
		if commonOpts.ExtraMeta != nil {
			for k, v := range commonOpts.ExtraMeta {
				doc.MetaData[k] = v
			}
		}
		docs = append(docs, doc)
	}

	return docs, nil
}
