package review

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/agent"
)

const dailyValidationPromptVersion = "daily-viewpoint-validation-v1"

type dailyValidationRun struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func DailyValidationWindow(summaryDate string) (string, time.Time, error) {
	day, err := time.ParseInLocation("2006-01-02", summaryDate, shanghaiLocation())
	if err != nil {
		return "", time.Time{}, errors.New("summary_date 必须是 YYYY-MM-DD")
	}
	verificationDay := nextTradingDay(day)
	return verificationDay.Format("2006-01-02"), verificationDay.Add(15 * time.Hour), nil
}

func ValidateDailyValidationSnapshot(summaryDate string, snapshot DailyValidationSnapshot) error {
	date, readyAt, err := DailyValidationWindow(summaryDate)
	if err != nil {
		return err
	}
	if snapshot.TradeDate != date {
		return fmt.Errorf("当前可用行情为 %s，与核验日 %s 不符；请使用核验日收盘数据", snapshot.TradeDate, date)
	}
	if !snapshot.CapturedAt.IsZero() && snapshot.CapturedAt.Before(readyAt) {
		return fmt.Errorf("核验日 %s 尚未收盘，不能使用盘中快照验证", date)
	}
	if snapshot.Intraday != nil && snapshot.Intraday.TradeDate != "" && snapshot.Intraday.TradeDate != date {
		return fmt.Errorf("盘面快照日期 %s 与核验日 %s 不符", snapshot.Intraday.TradeDate, date)
	}
	if snapshot.LimitUp.CurrentTradeDate != "" && snapshot.LimitUp.CurrentTradeDate != date {
		return fmt.Errorf("涨停梯队日期 %s 与核验日 %s 不符", snapshot.LimitUp.CurrentTradeDate, date)
	}
	for _, index := range snapshot.Indexes {
		if index.TradeDate != "" && index.TradeDate != date {
			return fmt.Errorf("指数 %s 行情日期 %s 与核验日 %s 不符", index.Name, index.TradeDate, date)
		}
	}
	for _, stock := range snapshot.Stocks {
		if stock.TradeDate != "" && stock.TradeDate != date {
			return fmt.Errorf("个股 %s 行情日期 %s 与核验日 %s 不符", stock.Name, stock.TradeDate, date)
		}
	}
	return nil
}

// DailyValidationCollector lets the HTTP layer collect provider-specific
// market data without coupling the review package to every market provider.
type DailyValidationCollector func(context.Context, DailySummary) (DailyValidationSnapshot, error)

func (a *Automation) LatestDailySummaryBefore(ctx context.Context, beforeDate string) (DailySummary, error) {
	if a == nil || a.store == nil {
		return DailySummary{}, errors.New("复盘日记存储不可用")
	}
	return a.store.LatestDailySummaryBefore(ctx, beforeDate)
}

func (a *Automation) GetDailyValidation(ctx context.Context, summaryDate string) (*DailyValidation, error) {
	if a == nil || a.store == nil {
		return nil, errors.New("复盘日记存储不可用")
	}
	validation, err := a.store.GetDailyValidation(ctx, strings.TrimSpace(summaryDate))
	if err != nil {
		return nil, err
	}
	date, _, err := DailyValidationWindow(summaryDate)
	if err != nil || validation.VerificationDate != date || ValidateDailyValidationSnapshot(summaryDate, validation.Snapshot) != nil {
		return nil, sql.ErrNoRows
	}
	return &validation, nil
}

func (a *Automation) GetDailyValidationJob(ctx context.Context, summaryDate string) (DailyValidationJob, error) {
	return a.getDailyValidationJob(ctx, strings.TrimSpace(summaryDate), time.Now())
}

func (a *Automation) getDailyValidationJob(ctx context.Context, summaryDate string, now time.Time) (DailyValidationJob, error) {
	if a == nil || a.store == nil {
		return DailyValidationJob{}, errors.New("复盘日记存储不可用")
	}
	date, readyAt, err := DailyValidationWindow(summaryDate)
	if err != nil {
		return DailyValidationJob{}, err
	}
	if _, err := a.store.GetDailySummary(ctx, summaryDate); errors.Is(err, sql.ErrNoRows) {
		return DailyValidationJob{SummaryDate: summaryDate, Status: "idle", Stage: "missing_summary", Message: "该日期没有已保存的 AI 复盘总结"}, nil
	} else if err != nil {
		return DailyValidationJob{}, err
	}
	a.dailyValidationMu.Lock()
	defer a.dailyValidationMu.Unlock()
	job, err := a.store.GetDailyValidationJob(ctx, summaryDate)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return DailyValidationJob{}, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		job = DailyValidationJob{SummaryDate: summaryDate, Status: "idle", Stage: "idle", Message: "该验证尚未运行，点击开始验证"}
	}
	validation, validationErr := a.GetDailyValidation(ctx, summaryDate)
	if validationErr != nil && !errors.Is(validationErr, sql.ErrNoRows) {
		return DailyValidationJob{}, validationErr
	}
	job.ResultAvailable = validation != nil
	if job.Status == "running" {
		run := a.dailyValidationRunning[summaryDate]
		if run == nil || run.ctx.Err() != nil || now.Sub(job.UpdatedAt) > 15*time.Minute {
			if run != nil {
				run.cancel()
				delete(a.dailyValidationRunning, summaryDate)
			}
			if validation != nil && !validation.GeneratedAt.Before(job.StartedAt) {
				job.Status, job.Stage, job.Message = "succeeded", "completed", "验证结果已保存到本机"
				job.CompletedAt = validation.GeneratedAt
			} else {
				job.Status, job.Stage = "failed", "interrupted"
				job.Error = "上次验证已中断或超时，请重新验证"
				job.Message, job.CompletedAt = job.Error, now.UTC()
			}
			job.UpdatedAt = now.UTC()
			if _, err := a.store.SaveDailyValidationJob(ctx, job); err != nil {
				return DailyValidationJob{}, err
			}
		}
	}
	if (job.Status == "idle" || job.Status == "succeeded") && validation != nil {
		job.Status, job.Stage, job.Message = "succeeded", "completed", "验证结果已保存到本机"
		job.Error = ""
		job.CompletedAt = validation.GeneratedAt
	} else if job.Status == "succeeded" {
		job.Status, job.Stage, job.Message = "idle", "idle", "验证已完成，但本机缓存不可用，请重新验证"
	}
	job.VerificationDate, job.ReadyAt = date, readyAt
	job.CanStart = !now.Before(readyAt) && job.Status != "running"
	if now.Before(readyAt) && job.Status != "running" && validation == nil {
		job.Status, job.Stage = "waiting", "waiting"
		job.Message = fmt.Sprintf("核验日 %s（%s）15:00 收盘后可开始验证", date, []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[readyAt.Weekday()])
	}
	return job, nil
}

func (a *Automation) StartDailyValidation(ctx context.Context, summaryDate string, force bool, collector DailyValidationCollector) (DailyValidationJob, error) {
	if a == nil || a.store == nil {
		return DailyValidationJob{}, errors.New("复盘日记存储不可用")
	}
	if collector == nil {
		return DailyValidationJob{}, errors.New("昨日验证行情采集器不可用")
	}
	summaryDate = strings.TrimSpace(summaryDate)
	if summaryDate == "" {
		return DailyValidationJob{}, errors.New("summary_date 不能为空")
	}
	summary, err := a.store.GetDailySummary(ctx, summaryDate)
	if err != nil {
		return DailyValidationJob{}, fmt.Errorf("读取昨日 AI 总结失败: %w", err)
	}
	if _, err := a.GetDailyValidationJob(ctx, summaryDate); err != nil {
		return DailyValidationJob{}, err
	}
	if !force {
		if validation, validationErr := a.GetDailyValidation(ctx, summaryDate); validationErr == nil {
			return DailyValidationJob{
				SummaryDate:      validation.SummaryDate,
				VerificationDate: validation.VerificationDate,
				Status:           "succeeded",
				Stage:            "completed",
				Message:          "昨日复盘验证已缓存",
				UpdatedAt:        validation.GeneratedAt,
				CompletedAt:      validation.GeneratedAt,
				ResultAvailable:  true,
				CanStart:         true,
			}, nil
		}
	}
	verificationDate, readyAt, err := DailyValidationWindow(summaryDate)
	if err != nil {
		return DailyValidationJob{}, err
	}
	if time.Now().Before(readyAt) {
		return DailyValidationJob{}, fmt.Errorf("核验日 %s 15:00 收盘后可开始验证", verificationDate)
	}

	a.dailyValidationMu.Lock()
	if a.dailyValidationRunning == nil {
		a.dailyValidationRunning = map[string]*dailyValidationRun{}
	}
	if a.dailyValidationRunning[summaryDate] != nil {
		a.dailyValidationMu.Unlock()
		return a.GetDailyValidationJob(ctx, summaryDate)
	}
	runCtx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	run := &dailyValidationRun{ctx: runCtx, cancel: cancel}
	a.dailyValidationRunning[summaryDate] = run
	defer a.dailyValidationMu.Unlock()

	now := time.Now().UTC()
	job := DailyValidationJob{
		SummaryDate:      summaryDate,
		Status:           "running",
		Stage:            "collecting",
		Message:          "正在采集验证日盘面数据",
		StartedAt:        now,
		UpdatedAt:        now,
		VerificationDate: verificationDate,
		ReadyAt:          readyAt,
	}
	if _, err := a.store.SaveDailyValidationJob(ctx, job); err != nil {
		cancel()
		delete(a.dailyValidationRunning, summaryDate)
		return DailyValidationJob{}, err
	}
	go a.runDailyValidation(summary, collector, run, job)
	return job, nil
}

func (a *Automation) runDailyValidation(summary DailySummary, collector DailyValidationCollector, run *dailyValidationRun, job DailyValidationJob) {
	defer func() {
		run.cancel()
		a.dailyValidationMu.Lock()
		if a.dailyValidationRunning[summary.TradeDate] == run {
			delete(a.dailyValidationRunning, summary.TradeDate)
		}
		a.dailyValidationMu.Unlock()
	}()
	ctx := run.ctx
	updateJob := func(job DailyValidationJob) {
		a.dailyValidationMu.Lock()
		defer a.dailyValidationMu.Unlock()
		if a.dailyValidationRunning[summary.TradeDate] == run {
			_, _ = a.store.SaveDailyValidationJob(context.Background(), job)
			if job.Status == "failed" || job.Status == "succeeded" {
				delete(a.dailyValidationRunning, summary.TradeDate)
				run.cancel()
			}
		}
	}
	snapshot, err := collector(ctx, summary)
	if err == nil {
		err = ValidateDailyValidationSnapshot(summary.TradeDate, snapshot)
	}
	if err != nil {
		job.Status = "failed"
		job.Stage = "failed"
		job.Error = err.Error()
		job.Message = "验证日盘面数据采集失败"
		job.UpdatedAt = time.Now().UTC()
		job.CompletedAt = job.UpdatedAt
		updateJob(job)
		return
	}
	job.VerificationDate = snapshot.TradeDate
	job.Stage = "evaluating"
	job.Message = "正在按触发条件核对昨日观点"
	job.UpdatedAt = time.Now().UTC()
	updateJob(job)

	validation := BuildDailyValidation(summary, snapshot)
	job.Stage = "explaining"
	job.Message = "正在生成验证结论与偏差解释"
	job.UpdatedAt = time.Now().UTC()
	updateJob(job)
	if a.prompter != nil {
		if err := EnrichDailyValidationWithAI(ctx, a.prompter, summary, snapshot, &validation); err != nil {
			validation.AIStatus = "rules"
			validation.AIMessage = "Hermes 验证解释不可用，已保留规则核验结果：" + err.Error()
		} else {
			validation.AIStatus = "ready"
		}
	} else {
		validation.AIStatus = "rules"
		validation.AIMessage = "Hermes 不可用，已保留规则核验结果"
	}
	validation.Score, validation.Coverage, validation.CorrectCount, validation.PartialCount, validation.WrongCount, validation.UnverifiedCount = scoreValidation(validation)
	validation.GeneratedAt = time.Now().UTC()
	a.dailyValidationMu.Lock()
	if a.dailyValidationRunning[summary.TradeDate] != run || ctx.Err() != nil {
		a.dailyValidationMu.Unlock()
		return
	}
	if _, err := a.store.SaveDailyValidation(context.Background(), validation); err != nil {
		a.dailyValidationMu.Unlock()
		job.Status = "failed"
		job.Stage = "failed"
		job.Error = err.Error()
		job.Message = "保存昨日复盘验证失败"
		job.UpdatedAt = time.Now().UTC()
		job.CompletedAt = job.UpdatedAt
		updateJob(job)
		return
	}
	a.dailyValidationMu.Unlock()
	job.Status = "succeeded"
	job.Stage = "completed"
	job.Message = "昨日复盘验证已完成并缓存"
	job.UpdatedAt = validation.GeneratedAt
	job.CompletedAt = validation.GeneratedAt
	updateJob(job)
}

// BuildDailyValidation performs all deterministic checks that do not require
// language-model interpretation. It is intentionally pure and easy to test.
func BuildDailyValidation(summary DailySummary, snapshot DailyValidationSnapshot) DailyValidation {
	validation := DailyValidation{
		SummaryDate:      summary.TradeDate,
		VerificationDate: snapshot.TradeDate,
		GeneratedAt:      time.Now().UTC(),
		PromptVersion:    dailyValidationPromptVersion,
		SummaryHash:      dailySummaryHash(summary),
		Status:           "completed",
		Snapshot:         snapshot,
		DataQuality:      append([]string(nil), snapshot.DataQuality...),
		Directions:       make([]DailyValidationDirectionResult, 0, len(summary.Directions)),
		Stocks:           make([]DailyValidationStockResult, 0, len(summary.TomorrowFocus)),
		Checklist:        make([]DailyValidationChecklistResult, 0, len(summary.VerificationChecklist)),
	}
	validation.Market = validateMarketRegime(summary.MarketRegime, snapshot)
	validation.Scenario = validateScenario(summary, snapshot)
	validation.ActualScenario = validation.Scenario.ActualName
	for _, direction := range summary.Directions {
		validation.Directions = append(validation.Directions, validateDirection(direction, snapshot))
	}
	for _, stock := range summary.TomorrowFocus {
		validation.Stocks = append(validation.Stocks, validateStock(stock, snapshot))
	}
	for _, item := range summary.VerificationChecklist {
		validation.Checklist = append(validation.Checklist, DailyValidationChecklistResult{Text: strings.TrimSpace(item), Verdict: "unverified", Evidence: []string{"验证清单需要结构化指标或 Hermes 解释"}})
	}
	validation.Score, validation.Coverage, validation.CorrectCount, validation.PartialCount, validation.WrongCount, validation.UnverifiedCount = scoreValidation(validation)
	validation.Headline = fmt.Sprintf("规则核验：市场判断%s，今日实际为%s", verdictLabel(validation.Market.Verdict), firstNonEmpty(validation.ActualScenario, "未知情景"))
	validation.Lessons = []string{"优先复盘触发条件和失效条件，不把单日涨跌直接等同于观点质量"}
	return validation
}

func dailySummaryHash(summary DailySummary) string {
	content, err := json.Marshal(summary)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func validateMarketRegime(expected string, snapshot DailyValidationSnapshot) DailyValidationMarketResult {
	actual := ""
	if snapshot.Emotion != nil {
		actual = snapshot.Emotion.Phase
	}
	if actual == "" && snapshot.Intraday != nil {
		actual = snapshot.Intraday.Status
	}
	if strings.TrimSpace(expected) == "" || actual == "" {
		return DailyValidationMarketResult{ExpectedRegime: expected, ActualPhase: actual, Verdict: "unverified", Summary: "缺少可比的昨日情绪判断或今日市场情绪数据", Evidence: []string{"市场情绪数据不完整"}}
	}
	verdict := "wrong"
	if validationRegimeMatch(expected, actual) {
		verdict = "correct"
	} else if validationRegimeDirection(expected) == validationRegimeDirection(actual) {
		verdict = "partial"
	}
	return DailyValidationMarketResult{
		ExpectedRegime: expected,
		ActualPhase:    actual,
		Verdict:        verdict,
		Summary:        fmt.Sprintf("昨日判断为%s，今日实际情绪为%s", expected, actual),
		Evidence:       marketEvidence(snapshot),
	}
}

func validationRegimeMatch(expected, actual string) bool {
	expected = validationRegimeDirection(expected)
	actual = validationRegimeDirection(actual)
	return expected != "" && expected == actual
}

func validationRegimeDirection(value string) string {
	value = strings.TrimSpace(value)
	switch {
	case strings.Contains(value, "退潮"), strings.Contains(value, "弱"), strings.Contains(value, "杀跌"):
		return "weak"
	case strings.Contains(value, "分歧"), strings.Contains(value, "震荡"), strings.Contains(value, "混沌"):
		return "mixed"
	case strings.Contains(value, "修复"), strings.Contains(value, "启动"), strings.Contains(value, "发酵"), strings.Contains(value, "主升"), strings.Contains(value, "高潮"), strings.Contains(value, "强"):
		return "strong"
	default:
		return ""
	}
}

func marketEvidence(snapshot DailyValidationSnapshot) []string {
	result := []string{}
	if snapshot.Emotion != nil {
		result = append(result, fmt.Sprintf("情绪%s：涨停%d、炸板%d、最高连板%d、昨日涨停平均收益%.2f%%", snapshot.Emotion.Phase, snapshot.Emotion.LimitUpCount, snapshot.Emotion.BrokenCount, snapshot.Emotion.MaxStreak, snapshot.Emotion.PreviousLimitUpRet))
	}
	if snapshot.Intraday != nil {
		result = append(result, fmt.Sprintf("盘中%s：高位平均收益%.2f%%、高位晋级率%.0f%%、风险分%.0f", snapshot.Intraday.Status, snapshot.Intraday.HighAverageReturn, snapshot.Intraday.HighAdvanceRate*100, snapshot.Intraday.RiskScore))
	}
	for _, index := range snapshot.Indexes {
		result = append(result, fmt.Sprintf("%s涨跌幅%.2f%%", index.Name, index.ChangePercent))
		if len(result) >= 4 {
			break
		}
	}
	return result
}

func validateScenario(summary DailySummary, snapshot DailyValidationSnapshot) DailyValidationScenarioResult {
	actualKey, actualName := actualScenario(snapshot)
	expectedKey, expectedName := expectedScenario(summary)
	verdict := "unverified"
	if expectedKey != "" && actualKey != "" {
		verdict = "wrong"
		if expectedKey == actualKey {
			verdict = "correct"
		} else if expectedKey == "base" || actualKey == "base" {
			verdict = "partial"
		}
	}
	return DailyValidationScenarioResult{
		ExpectedKey: expectedKey, ExpectedName: expectedName, ActualKey: actualKey, ActualName: actualName,
		Verdict: verdict,
		Summary: fmt.Sprintf("今日盘面按情绪、指数和连板结构归类为%s", actualName),
	}
}

func expectedScenario(summary DailySummary) (string, string) {
	value := strings.ToLower(summary.TomorrowOutlook + " " + summary.ExecutiveSummary)
	switch {
	case strings.Contains(value, "偏弱"), strings.Contains(value, "退潮"), strings.Contains(value, "防守"):
		return "weak", "偏弱情景"
	case strings.Contains(value, "偏强"), strings.Contains(value, "走强"), strings.Contains(value, "进攻"), strings.Contains(value, "主升"):
		return "strong", "偏强情景"
	case strings.TrimSpace(summary.TomorrowOutlook) != "":
		return "base", "基础情景"
	default:
		return "", ""
	}
}

func actualScenario(snapshot DailyValidationSnapshot) (string, string) {
	phase := ""
	if snapshot.Emotion != nil {
		phase = snapshot.Emotion.Phase
	}
	averageIndex := 0.0
	if len(snapshot.Indexes) > 0 {
		for _, index := range snapshot.Indexes {
			averageIndex += index.ChangePercent
		}
		averageIndex /= float64(len(snapshot.Indexes))
	}
	switch {
	case strings.Contains(phase, "退潮"), strings.Contains(phase, "强分歧"), averageIndex <= -1.2:
		return "weak", "偏弱情景"
	case strings.Contains(phase, "启动"), strings.Contains(phase, "修复"), strings.Contains(phase, "发酵"), strings.Contains(phase, "主升"), strings.Contains(phase, "高潮"), averageIndex >= 1.2:
		return "strong", "偏强情景"
	default:
		return "base", "基础情景"
	}
}

func validateDirection(direction DailyDirectionView, snapshot DailyValidationSnapshot) DailyValidationDirectionResult {
	name := strings.TrimSpace(direction.Name)
	bestRank, best := 0, DailyValidationTheme{}
	for index, theme := range snapshot.Themes {
		if validationNameMatch(name, theme.Name) {
			bestRank, best = index+1, theme
			break
		}
	}
	if bestRank == 0 {
		return DailyValidationDirectionResult{Name: name, Verdict: "unverified", Evidence: []string{"今日题材数据中未找到同名或可映射方向"}}
	}
	verdict := "wrong"
	score := 0
	if bestRank <= 3 && best.ChangePercent >= 0 {
		verdict, score = "correct", 100
	} else if bestRank <= 10 || best.ChangePercent >= 0 {
		verdict, score = "partial", 55
	}
	return DailyValidationDirectionResult{
		Name: name, Verdict: verdict, Score: score, Rank: bestRank, ActualChange: best.ChangePercent,
		Evidence: []string{fmt.Sprintf("今日题材排名%d，涨跌幅%.2f%%，上涨家数%d，涨停%d，阶段%s", bestRank, best.ChangePercent, best.RisingCount, best.LimitUpCount, firstNonEmpty(best.Stage, "未标注"))},
	}
}

func validationNameMatch(left, right string) bool {
	left = validationNormalizeName(left)
	right = validationNormalizeName(right)
	return left != "" && right != "" && (left == right || strings.Contains(left, right) || strings.Contains(right, left))
}

func validationNormalizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, token := range []string{"题材", "概念", "板块", "方向", "主线", "产业链"} {
		value = strings.ReplaceAll(value, token, "")
	}
	value = strings.NewReplacer(" ", "", "-", "", "_", "", "·", "", "/", "").Replace(value)
	return value
}

func validateStock(view DailyStockView, snapshot DailyValidationSnapshot) DailyValidationStockResult {
	name := strings.TrimSpace(view.Name)
	var actual *DailyValidationStock
	for index := range snapshot.Stocks {
		item := &snapshot.Stocks[index]
		if (strings.TrimSpace(view.Symbol) != "" && item.Symbol == strings.TrimSpace(view.Symbol)) || (view.Symbol == "" && validationNameMatch(name, item.Name)) {
			actual = item
			break
		}
	}
	if actual == nil {
		return DailyValidationStockResult{Name: name, Symbol: view.Symbol, Verdict: "unverified", Summary: "未能将昨日关注对象映射到今日行情", Evidence: []string{"缺少明确股票代码或今日行情覆盖不足"}}
	}
	openChange := 0.0
	if actual.PreviousClose > 0 {
		openChange = (actual.Open/actual.PreviousClose - 1) * 100
	}
	verdict := "partial"
	switch {
	case actual.ChangePercent >= 2:
		verdict = "correct"
	case actual.ChangePercent <= -3:
		verdict = "wrong"
	}
	return DailyValidationStockResult{
		Name: name, Symbol: actual.Symbol, Verdict: verdict, ActualChange: actual.ChangePercent, OpenChange: openChange,
		IntradayHigh: actual.High, IntradayLow: actual.Low,
		Summary:  fmt.Sprintf("今日收盘涨跌幅%.2f%%，竞价/开盘相对昨收%.2f%%，日内高低点%.2f/%.2f", actual.ChangePercent, openChange, actual.High, actual.Low),
		Evidence: []string{fmt.Sprintf("%s（%s）行情已匹配，来源%s", actual.Name, actual.Symbol, firstNonEmpty(actual.Source, "未标注"))},
	}
}

func scoreValidation(validation DailyValidation) (score, coverage float64, correct, partial, wrong, unverified int) {
	type scored struct {
		verdict string
		weight  float64
	}
	items := []scored{{validation.Market.Verdict, 25}, {validation.Scenario.Verdict, 20}}
	for _, item := range validation.Directions {
		items = append(items, scored{item.Verdict, 25 / math.Max(float64(len(validation.Directions)), 1)})
	}
	for _, item := range validation.Stocks {
		items = append(items, scored{item.Verdict, 20 / math.Max(float64(len(validation.Stocks)), 1)})
	}
	for _, item := range validation.Checklist {
		items = append(items, scored{item.Verdict, 10 / math.Max(float64(len(validation.Checklist)), 1)})
	}
	for _, item := range items {
		switch item.verdict {
		case "correct":
			correct++
			score += item.weight
			coverage += item.weight
		case "partial":
			partial++
			score += item.weight * .5
			coverage += item.weight
		case "wrong":
			wrong++
			coverage += item.weight
		default:
			unverified++
		}
	}
	if coverage > 0 {
		score = score / coverage * 100
	}
	return math.Round(score*10) / 10, math.Round(coverage*10) / 10, correct, partial, wrong, unverified
}

func verdictLabel(value string) string {
	switch value {
	case "correct":
		return "正确"
	case "partial":
		return "部分正确"
	case "wrong":
		return "错误"
	default:
		return "暂不可验证"
	}
}

type dailyValidationAIModel struct {
	Headline        string   `json:"headline"`
	ActualScenario  string   `json:"actual_scenario"`
	ScenarioVerdict string   `json:"scenario_verdict"`
	ScenarioSummary string   `json:"scenario_summary"`
	MarketSummary   string   `json:"market_summary"`
	Lessons         []string `json:"lessons"`
	RealizedRisks   []string `json:"realized_risks"`
}

func EnrichDailyValidationWithAI(ctx context.Context, prompter agent.Prompter, summary DailySummary, snapshot DailyValidationSnapshot, validation *DailyValidation) error {
	if prompter == nil || validation == nil {
		return errors.New("验证解释器不可用")
	}
	payload := struct {
		Summary   DailySummary            `json:"yesterday_summary"`
		Snapshot  DailyValidationSnapshot `json:"today_snapshot"`
		RuleCheck DailyValidation         `json:"rule_check"`
	}{Summary: summary, Snapshot: snapshot, RuleCheck: *validation}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	prompt := `你是谨慎的A股复盘验证器。请基于“昨日已经存在的AI总结”和“今日实际盘面快照”解释观点是否兑现。
规则：
1. 只能使用输入中的事实；不得补造指数、个股、题材、资金或新闻数据。
2. 规则核验结果是底稿，不能把规则未验证的观点说成已验证。
3. 区分正确、部分正确、错误、无法验证；单日涨跌不能单独证明观点质量。
4. 重点解释触发条件、失效条件、情景路径和最大偏差，不给出买卖指令。
5. 只返回严格JSON：{"headline":"...","actual_scenario":"base|strong|weak","scenario_verdict":"correct|partial|wrong|unverified","scenario_summary":"...","market_summary":"...","lessons":["..."],"realized_risks":["..."]}

输入JSON：` + string(data)
	result, err := agent.PromptFullyAuthorized(agent.WithUsageModule(ctx, "review-validation"), prompter, prompt)
	if err != nil {
		return err
	}
	var model dailyValidationAIModel
	if err := json.Unmarshal([]byte(jsonObject(result.Content)), &model); err != nil {
		return fmt.Errorf("Hermes 昨日验证未返回有效 JSON: %w", err)
	}
	if strings.TrimSpace(model.Headline) != "" {
		validation.Headline = strings.TrimSpace(model.Headline)
	}
	if model.ActualScenario == "base" || model.ActualScenario == "strong" || model.ActualScenario == "weak" {
		validation.ActualScenario = scenarioLabel(model.ActualScenario)
		validation.Scenario.ActualKey = model.ActualScenario
		validation.Scenario.ActualName = scenarioLabel(model.ActualScenario)
	}
	if model.ScenarioVerdict == "correct" || model.ScenarioVerdict == "partial" || model.ScenarioVerdict == "wrong" || model.ScenarioVerdict == "unverified" {
		validation.Scenario.Verdict = model.ScenarioVerdict
	}
	if strings.TrimSpace(model.ScenarioSummary) != "" {
		validation.Scenario.Summary = strings.TrimSpace(model.ScenarioSummary)
	}
	if strings.TrimSpace(model.MarketSummary) != "" {
		validation.Market.Summary = strings.TrimSpace(model.MarketSummary)
	}
	validation.Lessons = cleanStringList(append(validation.Lessons, model.Lessons...))
	validation.RealizedRisks = cleanStringList(model.RealizedRisks)
	return nil
}

func scenarioLabel(key string) string {
	switch key {
	case "strong":
		return "偏强情景"
	case "weak":
		return "偏弱情景"
	case "base":
		return "基础情景"
	default:
		return "未知情景"
	}
}

// Keep deterministic result ordering when providers return maps or parallel
// responses in a different order.
func sortValidationThemes(items []DailyValidationTheme) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].ChangePercent != items[j].ChangePercent {
			return items[i].ChangePercent > items[j].ChangePercent
		}
		return items[i].Name < items[j].Name
	})
}
