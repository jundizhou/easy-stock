// Command shannon-dump 是 Brian Shannon 六层规则的 Go 侧结果导出器。
//
// 用途：与 Python 侧（brian_shannon_system/tools/shannon_dump.py）做双端对拍。
// 两端都用同一套合成/真实行情、同一组参数，把六层结果导出成 JSON 后逐字段比对。
//
// 用法：
//
//	go run ./cmd/shannon-dump -codes 600519,000001 -equity 100000 -out go.json
//	go run ./cmd/shannon-dump -limit 200 -equity 100000 -out go.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"easy-stock/backend/internal/providers/stockdb"
	"easy-stock/backend/internal/strategy/shannon"
)

func main() {
	codesFlag := flag.String("codes", "", "逗号分隔的股票代码；留空则按 -universe 解析")
	codesFile := flag.String("codes-file", "", "从文件读代码（每行一个），与 -codes 合并")
	universe := flag.String("universe", "all", "股票池：all（全市场）或板块名")
	limit := flag.Int("limit", 0, "最多扫描多少只（0 = 不限）")
	equity := flag.Float64("equity", shannon.DefaultEquity, "账户净值（元）")
	base := flag.String("base", "", "stockdb 基址，默认 http://127.0.0.1:7899")
	workers := flag.Int("workers", 8, "并发数")
	out := flag.String("out", "", "输出文件（默认 stdout）")
	flag.Parse()

	client := stockdb.NewClient(stockdb.WithBaseURL(*base))
	feed := shannon.NewFeed(client)
	cfg := shannon.DefaultConfig()

	var codes []string
	for _, c := range strings.Split(*codesFlag, ",") {
		if c = strings.TrimSpace(c); c != "" {
			codes = append(codes, c)
		}
	}
	if *codesFile != "" {
		blob, err := os.ReadFile(*codesFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "shannon-dump: read codes: %v\n", err)
			os.Exit(1)
		}
		for _, ln := range strings.Split(string(blob), "\n") {
			if c := strings.TrimSpace(ln); c != "" {
				codes = append(codes, c)
			}
		}
	}
	req := shannon.ScanRequest{
		Codes:    codes,
		Universe: *universe,
		Limit:    *limit,
		Equity:   *equity,
		Workers:  *workers,
	}
	// -universe 传非 all 时按板块名解析（stockdb 板块* 表只有概念/申万行业）。
	if len(codes) == 0 && strings.TrimSpace(*universe) != "" && strings.TrimSpace(*universe) != "all" {
		req.Board = strings.TrimSpace(*universe)
	}

	resp, err := feed.Scan(context.Background(), req, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shannon-dump: %v\n", err)
		os.Exit(1)
	}
	resp.GeneratedAt = time.Now().Format("2006-01-02 15:04:05")

	blob, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "shannon-dump: encode: %v\n", err)
		os.Exit(1)
	}
	if *out != "" {
		if err := os.WriteFile(*out, blob, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "shannon-dump: write: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "shannon-dump: %d 只 → %s\n", resp.Count, *out)
		return
	}
	os.Stdout.Write(blob)
}
