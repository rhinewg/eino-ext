package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/cloudwego/eino-ext/components/document/parser/pdf"
)

func main() {
	ctx := context.Background()
	// 直接使用绝对路径
	pdfPath := `d:\teardown\costall\costbox\lib\eino-ext\components\document\parser\pdf\testdata\test_pdf.pdf`

	fmt.Println("========== PDF Parser 功能测试 ==========")
	fmt.Println()

	// 测试 1: 获取 PDF 页数
	fmt.Println("[测试 1] 获取 PDF 页数...")
	pageCount, err := pdf.GetPDFPageCount(ctx, pdfPath)
	if err != nil {
		fmt.Printf("❌ 失败: %v\n", err)
	} else {
		fmt.Printf("✅ 成功: PDF 共有 %d 页\n", pageCount)
	}
	fmt.Println()

	// 测试 2: 纯文本提取
	fmt.Println("[测试 2] 纯文本提取...")
	parser1, err := pdf.NewPDFParser(ctx, nil)
	if err != nil {
		fmt.Printf("❌ 创建解析器失败: %v\n", err)
		return
	}

	f1, err := os.Open(pdfPath)
	if err != nil {
		fmt.Printf("❌ 打开文件失败: %v\n", err)
		return
	}
	defer f1.Close()

	docs1, err := parser1.Parse(ctx, f1)
	if err != nil {
		fmt.Printf("❌ 解析失败: %v\n", err)
	} else {
		fmt.Printf("✅ 成功: 提取了 %d 个文档\n", len(docs1))
		if len(docs1) > 0 {
			contentLen := len(docs1[0].Content)
			fmt.Printf("   内容长度: %d 字符\n", contentLen)
			if contentLen > 100 {
				fmt.Printf("   内容预览: %s...\n", docs1[0].Content[:100])
			} else {
				fmt.Printf("   内容: %s\n", docs1[0].Content)
			}
		}
	}
	fmt.Println()

	// 测试 3: 按页提取文本
	fmt.Println("[测试 3] 按页提取文本...")
	f2, err := os.Open(pdfPath)
	if err != nil {
		fmt.Printf("❌ 打开文件失败: %v\n", err)
		return
	}
	defer f2.Close()

	docs2, err := parser1.Parse(ctx, f2, pdf.WithToPages(true))
	if err != nil {
		fmt.Printf("❌ 解析失败: %v\n", err)
	} else {
		fmt.Printf("✅ 成功: 提取了 %d 页\n", len(docs2))
		for i, doc := range docs2 {
			fmt.Printf("   第 %d 页: %d 字符\n", i+1, len(doc.Content))
		}
	}
	fmt.Println()

	// 测试 4: PDF 转图片
	fmt.Println("[测试 4] PDF 转图片（JPEG）...")
	images, err := pdf.ExtractPDFPagesAsImages(ctx, pdfPath, &pdf.ExtractPDFOptions{
		DPI: 100,
	})
	if err != nil {
		fmt.Printf("❌ 转换失败: %v\n", err)
	} else {
		fmt.Printf("✅ 成功: 生成了 %d 张图片\n", len(images))
		for _, img := range images {
			base64Preview := base64.StdEncoding.EncodeToString(img.JPEGData[:min(50, len(img.JPEGData))])
			fmt.Printf("   第 %d 页: %d 字节, Base64 预览: %s...\n", 
				img.PageNumber, img.FileSize, base64Preview[:30])
		}
	}
	fmt.Println()

	// 测试 5: 页范围解析
	fmt.Println("[测试 5] 页范围解析测试...")
	testCases := []string{"1", "1-2", "1-", "-2"}
	for _, rangeStr := range testCases {
		opts, err := pdf.ParsePDFPageRange(rangeStr)
		if err != nil {
			fmt.Printf("   ❌ '%s' 解析失败: %v\n", rangeStr, err)
		} else {
			fmt.Printf("   ✅ '%s' → FirstPage=%d, LastPage=%d\n", 
				rangeStr, opts.FirstPage, opts.LastPage)
		}
	}
	fmt.Println()

	// 测试 6: 渲染池
	fmt.Println("[测试 6] 渲染池状态...")
	pool := pdf.GetDefaultPool()
	stats := pool.GetStats()
	fmt.Printf("✅ 渲染池: 最大并发=%d, 当前活跃=%d, 可用=%d, 累计完成=%d\n",
		stats.MaxConcurrent, stats.ActiveTasks, stats.Available, stats.TotalCompleted)
	fmt.Println()

	// 测试 7: 使用 Parser 接口转图片
	fmt.Println("[测试 7] Parser 接口转图片...")
	f3, err := os.Open(pdfPath)
	if err != nil {
		fmt.Printf("❌ 打开文件失败: %v\n", err)
		return
	}
	defer f3.Close()

	parser2, err := pdf.NewPDFParser(ctx, &pdf.Config{
		ExtractAsImages: true,
		DPI:             72,
	})
	if err != nil {
		fmt.Printf("❌ 创建解析器失败: %v\n", err)
		return
	}

	docs3, err := parser2.Parse(ctx, f3, pdf.WithExtractAsImages(true))
	if err != nil {
		fmt.Printf("❌ 解析失败: %v\n", err)
	} else {
		fmt.Printf("✅ 成功: 提取了 %d 个文档（图片模式）\n", len(docs3))
		for _, doc := range docs3 {
			pageNum := doc.MetaData["page_number"]
			imageSize := doc.MetaData["image_size"]
			contentType := doc.MetaData["content_type"]
			fmt.Printf("   页码: %v, 大小: %v 字节, 类型: %v\n", pageNum, imageSize, contentType)
		}
	}
	fmt.Println()

	// 测试 8: 指定页范围提取
	fmt.Println("[测试 8] 指定页范围提取（第 1 页）...")
	f4, err := os.Open(pdfPath)
	if err != nil {
		fmt.Printf("❌ 打开文件失败: %v\n", err)
		return
	}
	defer f4.Close()

	docs4, err := parser1.Parse(ctx, f4, pdf.WithPageRange("1"))
	if err != nil {
		fmt.Printf("❌ 解析失败: %v\n", err)
	} else {
		fmt.Printf("✅ 成功: 提取了 %d 页\n", len(docs4))
		if len(docs4) > 0 {
			pageNum := docs4[0].MetaData["page_number"]
			fmt.Printf("   页码: %v\n", pageNum)
		}
	}
	fmt.Println()

	fmt.Println("========== 所有测试完成 ==========")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
