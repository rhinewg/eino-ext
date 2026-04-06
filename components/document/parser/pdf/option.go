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

import "github.com/cloudwego/eino/components/document/parser"

type options struct {
	toPages         *bool
	extractAsImages *bool
	dpi             *float64
	pageRange       *string
	imageMaxPages   *int
}

// WithToPages is a parser option that specifies whether to parse the PDF into pages.
func WithToPages(toPages bool) parser.Option {
	return parser.WrapImplSpecificOptFn(func(opts *options) {
		opts.toPages = &toPages
	})
}

// WithExtractAsImages is a parser option that specifies whether to extract PDF pages as images.
// 当设置为 true 时，PDF 页面将渲染为 JPEG 图片，适用于多模态大模型
func WithExtractAsImages(extract bool) parser.Option {
	return parser.WrapImplSpecificOptFn(func(opts *options) {
		opts.extractAsImages = &extract
	})
}

// WithDPI is a parser option that specifies the DPI for image rendering.
// 仅在 ExtractAsImages=true 时生效，默认值为 PDFDefaultDPI (100)
func WithDPI(dpi float64) parser.Option {
	return parser.WrapImplSpecificOptFn(func(opts *options) {
		opts.dpi = &dpi
	})
}

// WithPageRange is a parser option that specifies which pages to extract.
// 支持的格式: "5", "1-10", "3-", "-5"
// 当设置此选项时，会自动启用 ExtractAsImages 模式
func WithPageRange(pageRange string) parser.Option {
	return parser.WrapImplSpecificOptFn(func(opts *options) {
		opts.pageRange = &pageRange
	})
}

// WithImageMaxPages is a parser option that specifies the maximum number of pages to extract.
// 默认值为 PDFMaxPagesPerRead (20)
func WithImageMaxPages(maxPages int) parser.Option {
	return parser.WrapImplSpecificOptFn(func(opts *options) {
		opts.imageMaxPages = &maxPages
	})
}
