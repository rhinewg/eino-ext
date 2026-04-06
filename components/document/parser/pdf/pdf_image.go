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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dslipak/pdf"
)

// PDF 转图片相关常量
const (
	// PDFMaxFileSize 最大可处理 PDF 文件大小 (50MB)
	PDFMaxFileSize = 50 * 1024 * 1024

	// PDFMaxPages 最大可处理 PDF 页数
	PDFMaxPages = 100

	// PDFMaxPagesPerRead 单次请求最大提取页数
	PDFMaxPagesPerRead = 20

	// PDFRenderTimeout PDF 渲染超时时间
	PDFRenderTimeout = 30 * time.Second

	// PDFDefaultDPI 默认渲染分辨率 (DPI)
	PDFDefaultDPI = 100.0
)

// PDFPageImage 表示单页 PDF 渲染结果
type PDFPageImage struct {
	PageNumber int     // 页码（从 1 开始）
	JPEGData   []byte  // JPEG 格式的字节数据
	FileSize   int     // JPEG 文件大小（字节）
}

// ExtractPDFOptions PDF 转图片选项
type ExtractPDFOptions struct {
	FirstPage  int     // 起始页（1-indexed），0 表示从第一页开始
	LastPage   int     // 结束页，0 表示到最后一页
	DPI        float64 // 分辨率，默认 PDFDefaultDPI
}

// PDFError PDF 处理错误
type PDFError struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func (e *PDFError) Error() string {
	return fmt.Sprintf("pdf error: %s - %s", e.Reason, e.Message)
}

// 常见错误类型
var (
	ErrPDFEmpty             = &PDFError{Reason: "empty", Message: "PDF file is empty"}
	ErrPDFTooLarge          = &PDFError{Reason: "too_large", Message: fmt.Sprintf("PDF file exceeds maximum size of %d MB", PDFMaxFileSize/(1024*1024))}
	ErrPDFTooManyPages      = &PDFError{Reason: "too_many_pages", Message: fmt.Sprintf("PDF has too many pages (max %d)", PDFMaxPages)}
	ErrPDFPasswordProtected = &PDFError{Reason: "password_protected", Message: "PDF is password protected"}
	ErrPDFCorrupted         = &PDFError{Reason: "corrupted", Message: "PDF file is corrupted or invalid"}
	ErrPDFToppmUnavailable  = &PDFError{Reason: "unavailable", Message: "pdftoppm not found, please install poppler-utils"}
)

// pdftoppmCache 缓存 pdftoppm 是否可用的检查结果
var (
	pdftoppmOnce  sync.Once
	pdftoppmAvail bool
)

// isPDFToppmAvailable 检查 pdftoppm 是否可用（带缓存）
func isPDFToppmAvailable() bool {
	pdftoppmOnce.Do(func() {
		_, err := exec.LookPath("pdftoppm")
		pdftoppmAvail = err == nil
	})
	return pdftoppmAvail
}

// ExtractPDFPagesAsImages 将 PDF 页面渲染为 JPEG 图片
// 依赖系统安装 poppler-utils (pdftoppm)
// 支持按页范围提取，适用于多模态大模型理解
func ExtractPDFPagesAsImages(ctx context.Context, pdfPath string, opts *ExtractPDFOptions) ([]PDFPageImage, error) {
	// 默认选项
	if opts == nil {
		opts = &ExtractPDFOptions{DPI: PDFDefaultDPI}
	}
	if opts.DPI <= 0 {
		opts.DPI = PDFDefaultDPI
	}

	// 1. 检查 pdftoppm 是否可用
	if !isPDFToppmAvailable() {
		return nil, ErrPDFToppmUnavailable
	}

	// 2. 检查 PDF 文件
	info, err := os.Stat(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat PDF file: %w", err)
	}
	if info.Size() == 0 {
		return nil, ErrPDFEmpty
	}
	if info.Size() > PDFMaxFileSize {
		return nil, ErrPDFTooLarge
	}

	// 3. 验证 PDF 文件头
	if err := validatePDFHeader(pdfPath); err != nil {
		return nil, err
	}

	// 4. 检查页数
	pageCount, err := GetPDFPageCount(ctx, pdfPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get PDF page count: %w", err)
	}
	if pageCount > PDFMaxPages {
		return nil, ErrPDFTooManyPages
	}

	// 5. 计算实际提取的页范围
	firstPage := 1
	lastPage := pageCount
	if opts.FirstPage > 0 {
		firstPage = opts.FirstPage
	}
	if opts.LastPage > 0 {
		lastPage = opts.LastPage
	}
	if lastPage > pageCount {
		lastPage = pageCount
	}

	// 检查页数范围有效性
	if firstPage > lastPage {
		return nil, fmt.Errorf("invalid page range: first=%d, last=%d", firstPage, lastPage)
	}
	if lastPage-firstPage+1 > PDFMaxPagesPerRead {
		return nil, fmt.Errorf("page range too large: max %d pages per request", PDFMaxPagesPerRead)
	}

	// 6. 创建临时输出目录
	tmpDir, err := os.MkdirTemp("", "pdf-extract-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	prefix := filepath.Join(tmpDir, "page")

	// 7. 构建 pdftoppm 命令
	// pdftoppm -jpeg -r 100 [-f N] [-l M] <input.pdf> <prefix>
	args := []string{"-jpeg", "-r", strconv.FormatFloat(opts.DPI, 'f', 0, 64)}
	if firstPage > 1 {
		args = append(args, "-f", strconv.Itoa(firstPage))
	}
	if lastPage < pageCount {
		args = append(args, "-l", strconv.Itoa(lastPage))
	}
	args = append(args, pdfPath, prefix)

	// 8. 执行渲染命令
	cmdCtx, cancel := context.WithTimeout(ctx, PDFRenderTimeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "pdftoppm", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		errStr := string(output)
		return nil, parsePDFToppmError(err, errStr)
	}

	// 9. 读取生成的 JPEG 文件
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read temp dir: %w", err)
	}

	var jpgFiles []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".jpg") {
			jpgFiles = append(jpgFiles, entry.Name())
		}
	}
	sort.Strings(jpgFiles) // 确保按页码顺序返回

	// 10. 加载图片数据
	images := make([]PDFPageImage, 0, len(jpgFiles))
	for i, jpgFile := range jpgFiles {
		data, err := os.ReadFile(filepath.Join(tmpDir, jpgFile))
		if err != nil {
			return nil, fmt.Errorf("failed to read JPEG file %s: %w", jpgFile, err)
		}
		images = append(images, PDFPageImage{
			PageNumber: firstPage + i,
			JPEGData:   data,
			FileSize:   len(data),
		})
	}

	return images, nil
}

// GetPDFPageCount 获取 PDF 文件总页数
func GetPDFPageCount(ctx context.Context, pdfPath string) (int, error) {
	if !isPDFToppmAvailable() {
		return 0, ErrPDFToppmUnavailable
	}

	// 尝试使用 pdfinfo
	if _, err := exec.LookPath("pdfinfo"); err == nil {
		return getPDFPageCountViaPDFInfo(ctx, pdfPath)
	}

	// 回退方案：使用 pdftoppm 尝试获取页数
	return getPDFPageCountViaPDFToppm(ctx, pdfPath)
}

// getPDFPageCountViaPDFInfo 通过 pdfinfo 获取页数
func getPDFPageCountViaPDFInfo(ctx context.Context, pdfPath string) (int, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "pdfinfo", pdfPath)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("pdfinfo failed: %w", err)
	}

	// 解析 "Pages:  N" 行
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "Pages:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				count, err := strconv.Atoi(parts[1])
				if err != nil {
					return 0, fmt.Errorf("failed to parse page count: %w", err)
				}
				return count, nil
			}
		}
	}
	return 0, fmt.Errorf("failed to parse page count from pdfinfo output")
}

// getPDFPageCountViaPDFToppm 通过 pdftoppm 回退获取页数
func getPDFPageCountViaPDFToppm(ctx context.Context, pdfPath string) (int, error) {
	// pdftoppm 本身不直接返回页数，这里使用 dslipak/pdf 库作为回退
	// 如果 pdftoppm 可用但 pdfinfo 不可用，我们用纯 Go 的 pdf 库来获取页数
	return getPDFPageCountViaDSLipak(pdfPath)
}

// validatePDFHeader 验证 PDF 文件头
func validatePDFHeader(pdfPath string) error {
	f, err := os.Open(pdfPath)
	if err != nil {
		return err
	}
	defer f.Close()

	header := make([]byte, 5)
	n, err := f.Read(header)
	if err != nil || n < 5 {
		return ErrPDFCorrupted
	}

	if string(header) != "%PDF-" {
		return ErrPDFCorrupted
	}
	return nil
}

// parsePDFToppmError 解析 pdftoppm 错误
func parsePDFToppmError(err error, output string) error {
	if output == "" {
		return fmt.Errorf("pdftoppm failed: %w", err)
	}

	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "password") || strings.Contains(lower, "encrypted"):
		return ErrPDFPasswordProtected
	case strings.Contains(lower, "damaged") || strings.Contains(lower, "corrupt") || strings.Contains(lower, "invalid"):
		return ErrPDFCorrupted
	case strings.Contains(lower, "no such file") || strings.Contains(lower, "cannot open"):
		return ErrPDFCorrupted
	default:
		return fmt.Errorf("pdftoppm failed: %s", output)
	}
}

// ParsePDFPageRange 解析页范围字符串
// 支持的格式:
//   - "5"     → {FirstPage: 5, LastPage: 5}
//   - "1-10"  → {FirstPage: 1, LastPage: 10}
//   - "3-"    → {FirstPage: 3, LastPage: 0} (0 表示到末尾)
//   - "-5"    → {FirstPage: 1, LastPage: 5}
func ParsePDFPageRange(pages string) (*ExtractPDFOptions, error) {
	pages = strings.TrimSpace(pages)
	if pages == "" {
		return nil, fmt.Errorf("empty page range string")
	}

	parts := strings.SplitN(pages, "-", 2)
	opts := &ExtractPDFOptions{}

	switch len(parts) {
	case 1:
		// 单页: "5"
		page, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid page number: %s", parts[0])
		}
		if page < 1 {
			return nil, fmt.Errorf("page number must be >= 1, got %d", page)
		}
		opts.FirstPage = page
		opts.LastPage = page

	case 2:
		// 范围: "1-10" 或 "3-" 或 "-5"
		if parts[0] != "" {
			first, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid first page: %s", parts[0])
			}
			if first < 1 {
				return nil, fmt.Errorf("first page must be >= 1, got %d", first)
			}
			opts.FirstPage = first
		}
		// else: FirstPage 保持 0，表示从第 1 页开始

		if parts[1] != "" {
			last, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid last page: %s", parts[1])
			}
			if last < 1 {
				return nil, fmt.Errorf("last page must be >= 1, got %d", last)
			}
			opts.LastPage = last
		}
		// else: LastPage 保持 0，表示到末尾
	}

	return opts, nil
}

// getPDFPageCountViaDSLipak 使用 dslipak/pdf 库获取页数（回退方案）
func getPDFPageCountViaDSLipak(pdfPath string) (int, error) {
	f, err := os.Open(pdfPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open PDF file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to stat PDF file: %w", err)
	}

	reader, err := pdf.NewReader(f, stat.Size())
	if err != nil {
		return 0, fmt.Errorf("failed to create PDF reader: %w", err)
	}

	return reader.NumPage(), nil
}
