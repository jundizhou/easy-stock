package dailyanalysis

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/runtimelog"
)

var ErrJobRunning = errors.New("已有自选股日报正在运行，请等待完成后再开始")

// KLineLoader 与 RealtimeLoader 由 httpapi 层注入，复用服务端已有的行情调用链。
type KLineLoader func(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error)
type RealtimeLoader func(ctx context.Context, symbols []string) ([]foundation.Quote, error)

type Service struct {
	store     *Store
	gateway   hermes.Gateway
	loadK     KLineLoader
	realtime  RealtimeLoader
	logger    *log.Logger
	client    *http.Client
	mu        sync.Mutex
	runningID string
}

func NewService(store *Store, gateway hermes.Gateway, loadK KLineLoader, realtime RealtimeLoader, logger *log.Logger) *Service {
	service := &Service{store: store, gateway: gateway, loadK: loadK, realtime: realtime, logger: logger, client: &http.Client{Timeout: 15 * time.Second}}
	if store != nil {
		_ = store.MarkInterrupted(context.Background())
	}
	return service
}

func (s *Service) Start(ctx context.Context, request Request, trigger string) (Job, error) {
	if s == nil || s.store == nil || s.loadK == nil {
		return Job{}, errors.New("自选股日报服务不可用")
	}
	symbols, err := normalizeSymbols(request.Symbols)
	if err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runningID != "" {
		return Job{}, ErrJobRunning
	}
	now := time.Now().UTC()
	job := Job{
		ID: newID(), Status: "running", Stage: "queued", Trigger: trigger, Request: Request{Symbols: symbols, AIEnhance: request.AIEnhance},
		TotalStocks: len(symbols), Message: "任务已提交，正在逐只计算技术指标与决策评分", StartedAt: now, UpdatedAt: now,
	}
	if _, err := s.store.Save(ctx, job); err != nil {
		return Job{}, err
	}
	s.runningID = job.ID
	if s.logger != nil {
		s.logger.Printf("level=info event=daily_analysis_start feature=daily-analysis job_id=%q trigger=%s stocks=%d ai_enhance=%t", job.ID, trigger, len(symbols), request.AIEnhance)
	}
	go s.run(job)
	return job, nil
}

func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	if s == nil || s.store == nil {
		return Job{}, errors.New("自选股日报服务不可用")
	}
	return s.store.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, limit int) ([]Job, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("自选股日报服务不可用")
	}
	return s.store.List(ctx, limit)
}

func (s *Service) Config(ctx context.Context) (Config, error) {
	if s == nil || s.store == nil {
		return Config{}, errors.New("自选股日报服务不可用")
	}
	return s.store.LoadConfig(ctx)
}

func (s *Service) UpdateConfig(ctx context.Context, config Config) (Config, error) {
	if s == nil || s.store == nil {
		return Config{}, errors.New("自选股日报服务不可用")
	}
	symbols, err := normalizeSymbols(config.Watchlist)
	if err != nil {
		return Config{}, err
	}
	config.Watchlist = symbols
	if config.RunHour < 0 || config.RunHour > 23 {
		return Config{}, errors.New("自动运行小时必须在 0-23 之间")
	}
	if config.RunMinute < 0 || config.RunMinute > 59 {
		return Config{}, errors.New("自动运行分钟必须在 0-59 之间")
	}
	if config.Push.Enabled && strings.TrimSpace(config.Push.WeComWebhook) == "" && strings.TrimSpace(config.Push.FeishuWebhook) == "" {
		return Config{}, errors.New("启用推送前请至少填写企业微信或飞书 Webhook 地址")
	}
	if strings.TrimSpace(config.Push.WeComWebhook) != "" {
		if err := validateWebhookURL(config.Push.WeComWebhook, wecomWebhookHost); err != nil {
			return Config{}, fmt.Errorf("企业微信 Webhook 无效: %w", err)
		}
	}
	if strings.TrimSpace(config.Push.FeishuWebhook) != "" {
		if err := validateWebhookURL(config.Push.FeishuWebhook, feishuWebhookHost); err != nil {
			return Config{}, fmt.Errorf("飞书 Webhook 无效: %w", err)
		}
	}
	return s.store.SaveConfig(ctx, config)
}

// 允许的推送目标域名白名单：推送 URL 会被服务端主动请求，
// 若允许任意地址将形成 SSRF（可被用来探测内网或把日报数据发往第三方）。
var (
	wecomWebhookHost  = "qyapi.weixin.qq.com"
	feishuWebhookHost = "open.feishu.cn"
)

func validateWebhookURL(rawURL string, allowedHost string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return errors.New("无法解析 URL")
	}
	if parsed.Scheme != "https" {
		return errors.New("必须使用 https://")
	}
	if parsed.User != nil || parsed.Port() != "" {
		return errors.New("不允许携带账号信息或自定义端口")
	}
	if parsed.Hostname() != allowedHost {
		return fmt.Errorf("仅支持官方域名 %s", allowedHost)
	}
	return nil
}

// Correlations 计算自选股两两 60 日收益相关性（皮尔逊）。
func (s *Service) Correlations(ctx context.Context, symbols []string, days int) (CorrelationMatrix, error) {
	if s == nil || s.loadK == nil {
		return CorrelationMatrix{}, errors.New("自选股日报服务不可用")
	}
	normalized, err := normalizeSymbols(symbols)
	if err != nil {
		return CorrelationMatrix{}, err
	}
	if len(normalized) < 2 {
		return CorrelationMatrix{}, errors.New("相关性矩阵至少需要两只股票")
	}
	if days <= 0 || days > 250 {
		days = 60
	}
	series := map[string][]float64{}
	errorsBySymbol := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4)
	for _, symbol := range normalized {
		symbol := symbol
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			lines, err := s.loadK(ctx, symbol, "day", days+10)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errorsBySymbol[symbol] = err.Error()
				return
			}
			series[symbol] = dailyReturns(lines, days)
		}()
	}
	wg.Wait()
	usable := make([]string, 0, len(normalized))
	for _, symbol := range normalized {
		if len(series[symbol]) >= days/2 {
			usable = append(usable, symbol)
		}
	}
	result := CorrelationMatrix{Symbols: usable, Days: days, Errors: errorsBySymbol}
	for i := 0; i < len(usable); i++ {
		for j := i + 1; j < len(usable); j++ {
			left, right := series[usable[i]], series[usable[j]]
			n := min(len(left), len(right))
			left, right = left[len(left)-n:], right[len(right)-n:]
			result.Pairs = append(result.Pairs, CorrelationPair{
				LeftSymbol: usable[i], RightSymbol: usable[j],
				Correlation: pearson(left, right), SampleDays: n,
			})
		}
	}
	return result, nil
}

func (s *Service) run(job Job) {
	started := time.Now()
	defer func() {
		s.mu.Lock()
		if s.runningID == job.ID {
			s.runningID = ""
		}
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	quotes := map[string]foundation.Quote{}
	if s.realtime != nil {
		if fetched, err := s.realtime(ctx, job.Request.Symbols); err == nil {
			for _, quote := range fetched {
				quotes[quote.Symbol] = quote
			}
		}
	}

	report := Report{ID: job.ID, Trigger: job.Trigger, PromptVersion: PromptVersion, AIEnhanced: false, Stocks: make([]StockReport, 0, len(job.Request.Symbols))}
	completed := 0
	for _, symbol := range job.Request.Symbols {
		job.Stage = "analyzing_stocks"
		job.CurrentSymbols = []string{symbol}
		job.Message = fmt.Sprintf("正在计算 %s 的技术指标与评分", symbol)
		s.persist(job)

		stockReport := StockReport{Symbol: symbol, Status: "failed", Error: "行情数据获取失败"}
		lines, err := s.loadK(ctx, symbol, "day", 160)
		if err == nil {
			var quote *foundation.Quote
			if value, ok := quotes[symbol]; ok {
				quote = &value
			}
			stockReport = buildStockReport(lines, quote, symbol)
		} else {
			stockReport.Error = err.Error()
			if s.logger != nil {
				s.logger.Printf("level=warn event=daily_analysis_stock_error feature=daily-analysis job_id=%q symbol=%s error=%q", job.ID, symbol, runtimelog.Redact(err.Error()))
			}
		}
		report.Stocks = append(report.Stocks, stockReport)
		completed++
		job.CompletedStocks = completed
		job.Message = fmt.Sprintf("已完成 %d/%d 只股票分析", completed, job.TotalStocks)
		s.persist(job)
	}

	report.TradeDate = latestTradeDate(report.Stocks)
	report.Summary = summarize(report.Stocks)

	if job.Request.AIEnhance && s.gateway != nil {
		job.Stage = "ai_enhancing"
		job.Message = "正在调用 AI 生成综合研判与个股点评"
		job.CurrentSymbols = nil
		s.persist(job)
		if err := s.enhanceWithAI(ctx, &report); err != nil {
			if s.logger != nil {
				s.logger.Printf("level=warn event=daily_analysis_ai_degraded feature=daily-analysis job_id=%q error=%q", job.ID, runtimelog.Redact(err.Error()))
			}
			report.Summary.Highlights = append(report.Summary.Highlights, "AI 增强不可用（"+err.Error()+"），已展示纯技术面结果")
		} else {
			report.AIEnhanced = true
		}
	}

	job.Stage = "pushing"
	job.Message = "正在推送日报"
	s.persist(job)
	config, _ := s.Config(ctx)
	if config.Push.Enabled {
		job.PushResults = s.pushReport(ctx, &report, config)
	}

	completedAt := time.Now().UTC()
	report.GeneratedAt = completedAt
	job.Report = &report
	job.ReportAvailable = true
	job.Stage = "completed"
	job.CurrentSymbols = nil
	job.UpdatedAt = completedAt
	job.CompletedAt = completedAt
	failed := 0
	for _, stock := range report.Stocks {
		if stock.Status != "succeeded" {
			failed++
		}
	}
	if failed == 0 {
		job.Status = "succeeded"
		job.Message = "自选股日报已生成，报告已保存在本机"
	} else {
		job.Status = "partial"
		job.Message = fmt.Sprintf("日报已生成，%d 只股票数据获取失败", failed)
		job.Error = fmt.Sprintf("%d 只股票分析失败", failed)
	}
	s.persist(job)
	if s.logger != nil {
		s.logger.Printf("level=info event=daily_analysis_complete feature=daily-analysis job_id=%q status=%s stocks=%d failed=%d ai_enhanced=%t duration_ms=%d", job.ID, job.Status, len(report.Stocks), failed, report.AIEnhanced, time.Since(started).Milliseconds())
	}
}

func (s *Service) enhanceWithAI(ctx context.Context, report *Report) error {
	status := s.gateway.Status()
	if !status.Available || !status.Configured {
		return errors.New("AI 模型未配置")
	}
	prompt := buildAIPrompt(report)
	response, err := hermes.PromptFullyAuthorized(ctx, s.gateway, prompt)
	if err != nil {
		return fmt.Errorf("AI 分析失败: %w", err)
	}
	var enhanced struct {
		Commentary string `json:"commentary"`
		Stocks     []struct {
			Symbol     string `json:"symbol"`
			Commentary string `json:"commentary"`
		} `json:"stocks"`
	}
	if err := decodeJSONObject(response.Content, &enhanced); err != nil {
		return fmt.Errorf("AI 未返回有效 JSON: %w", err)
	}
	if strings.TrimSpace(enhanced.Commentary) == "" {
		return errors.New("AI 返回缺少综合研判")
	}
	report.Summary.Highlights = append([]string{enhanced.Commentary}, report.Summary.Highlights...)
	for index := range report.Stocks {
		for _, item := range enhanced.Stocks {
			if strings.EqualFold(strings.TrimSpace(item.Symbol), report.Stocks[index].Symbol) {
				report.Stocks[index].Commentary = strings.TrimSpace(item.Commentary)
			}
		}
	}
	return nil
}

func (s *Service) pushReport(ctx context.Context, report *Report, config Config) []PushResult {
	results := make([]PushResult, 0, 2)
	if webhook := strings.TrimSpace(config.Push.WeComWebhook); webhook != "" {
		results = append(results, s.pushWeCom(ctx, report, webhook))
	}
	if webhook := strings.TrimSpace(config.Push.FeishuWebhook); webhook != "" {
		results = append(results, s.pushFeishu(ctx, report, webhook))
	}
	return results
}

func (s *Service) pushWeCom(ctx context.Context, report *Report, webhook string) PushResult {
	result := PushResult{Channel: "wecom"}
	markdown := renderMarkdown(report, true)
	payload, err := json.Marshal(map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": markdown}})
	if err != nil {
		result.Message = err.Error()
		return result
	}
	if err := s.postWebhook(ctx, webhook, payload); err != nil {
		result.Message = err.Error()
		if s.logger != nil {
			s.logger.Printf("level=warn event=daily_analysis_push_error feature=daily-analysis channel=wecom error=%q", runtimelog.Redact(err.Error()))
		}
		return result
	}
	result.OK = true
	result.Message = "企业微信推送成功"
	return result
}

func (s *Service) pushFeishu(ctx context.Context, report *Report, webhook string) PushResult {
	result := PushResult{Channel: "feishu"}
	payload, err := json.Marshal(map[string]any{"msg_type": "text", "content": map[string]string{"text": renderMarkdown(report, false)}})
	if err != nil {
		result.Message = err.Error()
		return result
	}
	if err := s.postWebhook(ctx, webhook, payload); err != nil {
		result.Message = err.Error()
		if s.logger != nil {
			s.logger.Printf("level=warn event=daily_analysis_push_error feature=daily-analysis channel=feishu error=%q", runtimelog.Redact(err.Error()))
		}
		return result
	}
	result.OK = true
	result.Message = "飞书推送成功"
	return result
}

func (s *Service) postWebhook(ctx context.Context, webhook string, payload []byte) error {
	// 发送前二次校验，防止历史配置或绕过 UpdateConfig 的路径写入非白名单地址。
	parsed, err := url.Parse(strings.TrimSpace(webhook))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" ||
		(parsed.Hostname() != wecomWebhookHost && parsed.Hostname() != feishuWebhookHost) {
		return errors.New("webhook 地址不在允许的官方域名内")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("webhook 返回状态码 %d", response.StatusCode)
	}
	return nil
}

// PushJob 对已完成的报告立即执行一次 Webhook 推送。
func (s *Service) PushJob(ctx context.Context, id string) (Job, error) {
	job, err := s.Get(ctx, id)
	if err != nil {
		return Job{}, err
	}
	if job.Report == nil {
		return Job{}, errors.New("该任务尚无报告可推送")
	}
	config, err := s.Config(ctx)
	if err != nil {
		return Job{}, err
	}
	if !config.Push.Enabled {
		return Job{}, errors.New("推送未启用，请先在日报配置中开启并填写 Webhook")
	}
	results := s.pushReport(ctx, job.Report, config)
	job.PushResults = results
	s.persist(job)
	return job, nil
}

// RunScheduler 收盘后按配置时间自动生成日报（daily_stock_analysis 的定时任务能力）。
func (s *Service) RunScheduler(ctx context.Context, logger *log.Logger) {
	if s == nil || s.store == nil {
		return
	}
	if logger != nil {
		logger.Printf("level=info event=scheduler_start feature=daily-analysis task=auto_report")
		defer logger.Printf("level=info event=scheduler_stop feature=daily-analysis task=auto_report")
	}
	for {
		config, err := s.store.LoadConfig(ctx)
		if err != nil {
			config = Config{}
		}
		next := nextRunTime(config)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if !config.AutoRun || len(config.Watchlist) == 0 {
			time.Sleep(time.Minute)
			continue
		}
		job, err := s.Start(ctx, Request{Symbols: config.Watchlist, AIEnhance: config.AIEnhance}, "scheduler")
		if err != nil {
			if logger != nil {
				logger.Printf("level=warn event=scheduler_error feature=daily-analysis task=auto_report error=%q", runtimelog.Redact(err.Error()))
			}
			time.Sleep(time.Minute)
		} else if logger != nil {
			logger.Printf("level=info event=scheduler_run feature=daily-analysis task=auto_report job_id=%q stocks=%d", job.ID, len(job.Request.Symbols))
		}
	}
}

func nextRunTime(config Config) time.Time {
	now := time.Now()
	if !config.AutoRun {
		return now.Add(time.Minute)
	}
	hour, minute := config.RunHour, config.RunMinute
	if hour < 0 || hour > 23 {
		hour = 15
	}
	if minute < 0 || minute > 59 {
		minute = 30
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func normalizeSymbols(symbols []string) ([]string, error) {
	if len(symbols) == 0 {
		return nil, errors.New("请至少添加一只自选股")
	}
	if len(symbols) > MaxSymbols {
		return nil, fmt.Errorf("自选股最多支持 %d 只", MaxSymbols)
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		parsed, err := foundation.NormalizeSymbol(symbol)
		if err != nil {
			return nil, fmt.Errorf("无效股票代码 %q: %w", symbol, err)
		}
		if _, exists := seen[parsed.Canonical]; exists {
			return nil, fmt.Errorf("股票 %s 重复添加", parsed.Canonical)
		}
		seen[parsed.Canonical] = struct{}{}
		normalized = append(normalized, parsed.Canonical)
	}
	return normalized, nil
}

func latestTradeDate(stocks []StockReport) string {
	for _, stock := range stocks {
		if stock.TradeDate != "" {
			return stock.TradeDate
		}
	}
	return time.Now().Format("2006-01-02")
}

func summarize(stocks []StockReport) MarketSummary {
	summary := MarketSummary{Highlights: []string{}}
	scores := make([]float64, 0, len(stocks))
	for _, stock := range stocks {
		if stock.Status != "succeeded" {
			summary.FailedCount++
			continue
		}
		scores = append(scores, float64(stock.Score))
		switch {
		case stock.Score >= 60:
			summary.BullCount++
		case stock.Score >= 40:
			summary.NeutralCount++
		default:
			summary.BearCount++
		}
		if stock.Score >= 75 {
			summary.Highlights = append(summary.Highlights, fmt.Sprintf("%s %s 评分 %d（%s）：%s", stock.Symbol, stock.Name, stock.Score, stock.Action, stock.Trend))
		}
	}
	if len(scores) > 0 {
		total := 0.0
		for _, score := range scores {
			total += score
		}
		summary.AverageScore = round(total/float64(len(scores)), 1)
	}
	switch {
	case summary.BullCount > summary.BearCount*2 && summary.BullCount > 0:
		summary.Bias = "整体偏多"
	case summary.BearCount > summary.BullCount*2 && summary.BearCount > 0:
		summary.Bias = "整体偏空"
	case summary.BullCount > summary.BearCount:
		summary.Bias = "略偏多"
	case summary.BearCount > summary.BullCount:
		summary.Bias = "略偏空"
	default:
		summary.Bias = "多空均衡"
	}
	return summary
}

// renderMarkdown 输出决策仪表盘风格的推送文案；wecom 为 true 时使用其 markdown 子集。
func renderMarkdown(report *Report, wecom bool) string {
	var builder strings.Builder
	if wecom {
		builder.WriteString(fmt.Sprintf("## 📈 easy-stock 自选股日报 %s\n", report.TradeDate))
	} else {
		builder.WriteString(fmt.Sprintf("📈 easy-stock 自选股日报 %s\n", report.TradeDate))
	}
	summary := report.Summary
	builder.WriteString(fmt.Sprintf("综合倾向：%s | 均分 %.1f | 偏多 %d / 中性 %d / 偏空 %d\n", summary.Bias, summary.AverageScore, summary.BullCount, summary.NeutralCount, summary.BearCount))
	if len(summary.Highlights) > 0 {
		limit := summary.Highlights
		if len(limit) > 3 {
			limit = limit[:3]
		}
		builder.WriteString("亮点：\n")
		for _, highlight := range limit {
			builder.WriteString("· " + highlight + "\n")
		}
	}
	builder.WriteString("\n个股速览：\n")
	for _, stock := range report.Stocks {
		if stock.Status != "succeeded" {
			continue
		}
		builder.WriteString(fmt.Sprintf("· %s %s 评分%d %s %+.2f%%（%s）\n", stock.Symbol, stock.Name, stock.Score, stock.Action, stock.ChangePercent, stock.Trend))
	}
	if report.AIEnhanced && len(summary.Highlights) > 0 {
		builder.WriteString("\nAI 综合研判：" + summary.Highlights[0] + "\n")
	}
	builder.WriteString("\n> 评分与建议仅为技术面参考，不构成投资建议")
	return builder.String()
}

func buildAIPrompt(report *Report) string {
	var builder strings.Builder
	builder.WriteString("你是 A 股投研助手。以下为自动生成的自选股技术面日报数据，请输出严格的 JSON（不要 markdown 代码块）：\n")
	builder.WriteString(fmt.Sprintf(`{"commentary":"80字以内综合研判，概括今日自选股整体结构与明日观察重点","stocks":[{"symbol":"代码","commentary":"40字以内点评，聚焦该股当前最关键的一个技术信号或风险"}]}` + "\n"))
	builder.WriteString(fmt.Sprintf("股票数量：%d，市场倾向：%s，平均分：%.1f\n", len(report.Stocks), report.Summary.Bias, report.Summary.AverageScore))
	for _, stock := range report.Stocks {
		if stock.Status != "succeeded" {
			continue
		}
		ind := stock.Indicators
		builder.WriteString(fmt.Sprintf("%s %s：评分%d/%s/%s，涨跌%.2f%%，MA5=%.2f MA20=%.2f MA60=%.2f，MACD柱=%.3f，KDJ %.1f/%.1f/%.1f，RSI14=%.1f，量比%.2f，支撑%.2f 压力%.2f；信号：%s；风险：%s\n",
			stock.Symbol, stock.Name, stock.Score, stock.Action, stock.Trend, stock.ChangePercent,
			ind.MA5, ind.MA20, ind.MA60, ind.MACDHist, ind.KDJk, ind.KDJd, ind.KDJj, ind.Rsi14, ind.VolumeRatio, ind.Support, ind.Resistance,
			strings.Join(stock.Signals, "；"), strings.Join(stock.Risks, "；")))
	}
	return builder.String()
}

func dailyReturns(lines []foundation.KLine, days int) []float64 {
	if len(lines) < 2 {
		return nil
	}
	returns := make([]float64, 0, len(lines)-1)
	for i := 1; i < len(lines); i++ {
		if lines[i-1].Close > 0 {
			returns = append(returns, (lines[i].Close-lines[i-1].Close)/lines[i-1].Close)
		}
	}
	if len(returns) > days {
		returns = returns[len(returns)-days:]
	}
	return returns
}

func pearson(left, right []float64) float64 {
	n := min(len(left), len(right))
	if n < 5 {
		return 0
	}
	left, right = left[:n], right[:n]
	var sumLeft, sumRight float64
	for i := 0; i < n; i++ {
		sumLeft += left[i]
		sumRight += right[i]
	}
	meanLeft, meanRight := sumLeft/float64(n), sumRight/float64(n)
	var covariance, varianceLeft, varianceRight float64
	for i := 0; i < n; i++ {
		dl, dr := left[i]-meanLeft, right[i]-meanRight
		covariance += dl * dr
		varianceLeft += dl * dl
		varianceRight += dr * dr
	}
	if varianceLeft == 0 || varianceRight == 0 {
		return 0
	}
	return covariance / (sqrt(varianceLeft) * sqrt(varianceRight))
}

func sqrt(value float64) float64 {
	if value <= 0 {
		return 0
	}
	x := value
	for i := 0; i < 40; i++ {
		x = (x + value/x) / 2
	}
	return x
}

func (s *Service) persist(job Job) {
	job.UpdatedAt = time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.store.Save(ctx, job); err != nil && s.logger != nil {
		s.logger.Printf("level=warn event=daily_analysis_persist_error feature=daily-analysis job_id=%q error=%q", job.ID, runtimelog.Redact(err.Error()))
	}
}

func newID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err == nil {
		return hex.EncodeToString(buffer)
	}
	return fmt.Sprintf("daily-%d", time.Now().UnixNano())
}

func decodeJSONObject(content string, target any) error {
	content = strings.TrimSpace(content)
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return errors.New("JSON object not found")
	}
	return json.Unmarshal([]byte(content[start:end+1]), target)
}
