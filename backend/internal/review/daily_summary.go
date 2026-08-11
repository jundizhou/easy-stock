package review

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	dailySummaryPromptVersion    = "daily-viewpoint-consensus-v3"
	maxDailySummaryAuthors       = 30
	maxDailySummaryPosts         = 1000
	maxAuthorSummaryConcurrency  = 3
	maxAuthorSummaryArticles     = 24
	maxAuthorSummaryArticleRunes = 2800
	maxAuthorSummaryTotalRunes   = 42000
)

type reviewFreshnessWindow struct {
	TradeDate string
	Start     time.Time
	End       time.Time
	Rule      string
}

type dailySummaryArticle struct {
	Index       int    `json:"index"`
	Source      string `json:"source"`
	Author      string `json:"author"`
	Title       string `json:"title"`
	PublishedAt string `json:"published_at"`
	Content     string `json:"content"`
}

type dailySummaryAuthorGroup struct {
	Key      string
	Author   string
	Source   string
	Posts    []Post
	LatestAt time.Time
}

type authorStockViewModel struct {
	Name         string   `json:"name"`
	Symbol       string   `json:"symbol"`
	Logic        string   `json:"logic"`
	Evidence     []string `json:"evidence"`
	Trigger      string   `json:"trigger"`
	Invalidation string   `json:"invalidation"`
	Risk         string   `json:"risk"`
}

type authorViewpointModel struct {
	CoreView             string                 `json:"core_view"`
	MarketInterpretation string                 `json:"market_interpretation"`
	ViewEvolution        []string               `json:"view_evolution"`
	Themes               []string               `json:"themes"`
	TodaySurprises       []authorStockViewModel `json:"today_surprises"`
	TomorrowFocus        []authorStockViewModel `json:"tomorrow_focus"`
	TomorrowOutlook      string                 `json:"tomorrow_outlook"`
	Catalysts            []string               `json:"catalysts"`
	Risks                []string               `json:"risks"`
	Confidence           string                 `json:"confidence"`
	Evidence             []string               `json:"evidence"`
}

type authorSummaryResult struct {
	Group    dailySummaryAuthorGroup
	View     DailyAuthorView
	Articles []dailySummaryArticle
	Sources  []DailySummarySource
	Err      error
}

type dailySummaryModel struct {
	ExecutiveSummary      string               `json:"executive_summary"`
	MarketRegime          string               `json:"market_regime"`
	MarketAnalysis        string               `json:"market_analysis"`
	MarketFramework       DailyMarketFramework `json:"market_framework"`
	Consensus             []DailyConsensus     `json:"consensus"`
	Disagreements         []DailyDisagreement  `json:"disagreements"`
	Scenarios             []DailyScenario      `json:"scenarios"`
	Directions            []DailyDirectionView `json:"directions"`
	TodaySurprises        []DailyStockView     `json:"today_surprises"`
	TomorrowFocus         []DailyStockView     `json:"tomorrow_focus"`
	TomorrowOutlook       string               `json:"tomorrow_outlook"`
	TomorrowPlaybook      DailyPlaybook        `json:"tomorrow_playbook"`
	Catalysts             []string             `json:"catalysts"`
	Risks                 []string             `json:"risks"`
	VerificationChecklist []string             `json:"verification_checklist"`
	Limitations           []string             `json:"limitations"`
}

type dailySummaryProgress func(stage string, completedAuthors, totalAuthors, articleCount int, message string)

func (a *Automation) GetTodaySummary(ctx context.Context) (*DailySummary, error) {
	if a.store == nil {
		return nil, errors.New("复盘日记存储不可用")
	}
	window := effectiveReviewWindow(time.Now())
	summary, err := a.store.GetDailySummary(ctx, window.TradeDate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// A prompt-version change means the old cache does not satisfy the current
	// author-first aggregation rules. Hide it so the next click regenerates it.
	if summary.PromptVersion != dailySummaryPromptVersion || summary.FreshnessRule != window.Rule {
		return nil, nil
	}
	return &summary, nil
}

func (a *Automation) GetTodaySummaryJob(ctx context.Context) (DailySummaryJob, error) {
	if a.store == nil {
		return DailySummaryJob{}, errors.New("复盘日记存储不可用")
	}
	window := effectiveReviewWindow(time.Now())
	job, err := a.store.GetDailySummaryJob(ctx, window.TradeDate)
	if errors.Is(err, sql.ErrNoRows) {
		if summary, summaryErr := a.GetTodaySummary(ctx); summaryErr != nil {
			return DailySummaryJob{}, summaryErr
		} else if summary != nil {
			return cachedDailySummaryJob(*summary), nil
		}
		return DailySummaryJob{TradeDate: window.TradeDate, Status: "idle", Stage: "idle", Message: "尚未生成今日大V观点总结"}, nil
	}
	if err != nil {
		return DailySummaryJob{}, err
	}
	summary, err := a.GetTodaySummary(ctx)
	if err != nil {
		return DailySummaryJob{}, err
	}
	if job.Status == "running" && !a.dailySummaryJobIsRunning() {
		if summary != nil && !summary.GeneratedAt.Before(job.StartedAt) {
			job = cachedDailySummaryJob(*summary)
		} else {
			job.Status = "failed"
			job.Stage = "interrupted"
			job.Error = "应用或后台服务曾重启，本次总结已中断，请重新开始"
			job.Message = job.Error
			job.CompletedAt = time.Now().UTC()
			job.UpdatedAt = job.CompletedAt
		}
		job, err = a.store.SaveDailySummaryJob(ctx, job)
		if err != nil {
			return DailySummaryJob{}, err
		}
	}
	job.SummaryAvailable = summary != nil
	if job.Status == "succeeded" && summary == nil {
		return DailySummaryJob{TradeDate: window.TradeDate, Status: "idle", Stage: "idle", Message: "文章时效窗口已更新，请重新生成今日总结"}, nil
	}
	return job, nil
}

func (a *Automation) StartTodaySummary(ctx context.Context, force bool) (DailySummaryJob, error) {
	if a.store == nil {
		return DailySummaryJob{}, errors.New("复盘日记存储不可用")
	}
	if a.prompter == nil {
		return DailySummaryJob{}, errors.New("Hermes AI 分析底座不可用，请先在设置中配置模型")
	}
	a.dailySummaryJobMu.Lock()
	defer a.dailySummaryJobMu.Unlock()

	window := effectiveReviewWindow(time.Now())
	if a.dailySummaryRunning {
		job, err := a.store.GetDailySummaryJob(ctx, window.TradeDate)
		if err == nil {
			return job, nil
		}
		return DailySummaryJob{TradeDate: window.TradeDate, Status: "running", Stage: "preparing", Message: "AI总结正在后台运行，可稍后回来查看"}, nil
	}
	if !force {
		summary, err := a.GetTodaySummary(ctx)
		if err != nil {
			return DailySummaryJob{}, err
		}
		if summary != nil {
			job := cachedDailySummaryJob(*summary)
			saved, saveErr := a.store.SaveDailySummaryJob(ctx, job)
			saved.SummaryAvailable = saveErr == nil
			return saved, saveErr
		}
	}

	now := time.Now().UTC()
	job := DailySummaryJob{
		TradeDate: window.TradeDate,
		Status:    "running",
		Stage:     "preparing",
		Message:   "任务已提交，预计需要几分钟；可以先浏览其他页面，稍后回来查看结果",
		StartedAt: now,
		UpdatedAt: now,
	}
	job, err := a.store.SaveDailySummaryJob(ctx, job)
	if err != nil {
		return DailySummaryJob{}, err
	}
	a.dailySummaryRunning = true
	go a.runDailySummaryJob(job)
	return job, nil
}

func cachedDailySummaryJob(summary DailySummary) DailySummaryJob {
	return DailySummaryJob{
		TradeDate:        summary.TradeDate,
		Status:           "succeeded",
		Stage:            "completed",
		CompletedAuthors: summary.AuthorCount,
		TotalAuthors:     summary.AuthorCount,
		ArticleCount:     summary.ArticleCount,
		Message:          "今日总结已完成，结果已缓存在本机",
		StartedAt:        summary.GeneratedAt,
		UpdatedAt:        summary.GeneratedAt,
		CompletedAt:      summary.GeneratedAt,
		SummaryAvailable: true,
	}
}

func (a *Automation) dailySummaryJobIsRunning() bool {
	a.dailySummaryJobMu.Lock()
	defer a.dailySummaryJobMu.Unlock()
	return a.dailySummaryRunning
}

func (a *Automation) runDailySummaryJob(job DailySummaryJob) {
	defer func() {
		a.dailySummaryJobMu.Lock()
		a.dailySummaryRunning = false
		a.dailySummaryJobMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	var progressMu sync.Mutex
	update := func(stage string, completedAuthors, totalAuthors, articleCount int, message string) {
		progressMu.Lock()
		defer progressMu.Unlock()
		job.Status = "running"
		job.Stage = stage
		job.CompletedAuthors = completedAuthors
		job.TotalAuthors = totalAuthors
		job.ArticleCount = articleCount
		job.Message = message
		job.Error = ""
		job.UpdatedAt = time.Now().UTC()
		persistCtx, persistCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer persistCancel()
		_, _ = a.store.SaveDailySummaryJob(persistCtx, job)
	}

	summary, err := a.summarizeToday(ctx, update)
	completedAt := time.Now().UTC()
	job.UpdatedAt = completedAt
	job.CompletedAt = completedAt
	if err != nil {
		job.Status = "failed"
		job.Stage = "failed"
		job.Error = err.Error()
		job.Message = "总结未完成，可稍后重新尝试"
	} else {
		job.Status = "succeeded"
		job.Stage = "completed"
		job.CompletedAuthors = job.TotalAuthors
		job.ArticleCount = summary.ArticleCount
		job.Message = "今日总结已完成，结果已缓存在本机"
		job.Error = ""
		job.SummaryAvailable = true
	}
	persistCtx, persistCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer persistCancel()
	_, _ = a.store.SaveDailySummaryJob(persistCtx, job)
}

func (a *Automation) SummarizeToday(ctx context.Context) (DailySummary, error) {
	return a.summarizeToday(ctx, nil)
}

func (a *Automation) summarizeToday(ctx context.Context, progress dailySummaryProgress) (DailySummary, error) {
	if a.store == nil {
		return DailySummary{}, errors.New("复盘日记存储不可用")
	}
	if a.prompter == nil {
		return DailySummary{}, errors.New("Hermes AI 分析底座不可用，请先在设置中配置模型")
	}
	a.dailySummaryMu.Lock()
	defer a.dailySummaryMu.Unlock()

	now := time.Now()
	window := effectiveReviewWindow(now)
	if progress != nil {
		progress("preparing", 0, 0, 0, "正在筛选具有时效性的文章并按作者分组")
	}
	posts, err := a.store.ListPostsBetween(ctx, window.Start, window.End.Add(time.Second), maxDailySummaryPosts)
	if err != nil {
		return DailySummary{}, err
	}
	if len(posts) == 0 {
		return DailySummary{}, errors.New("有效时段内还没有可总结的大V文章，请先同步或导入近期文章")
	}
	allGroups := dailySummaryAuthorGroups(posts)
	if len(allGroups) == 0 {
		return DailySummary{}, errors.New("有效时段内的文章正文不足，暂时无法生成观点总结")
	}
	selectedGroups := selectDailySummaryAuthorGroups(allGroups)
	if progress != nil {
		progress("authors", 0, len(selectedGroups), len(posts), fmt.Sprintf("正在逐位归纳作者观点，共%d位作者", len(selectedGroups)))
	}
	results := a.summarizeDailyAuthors(ctx, selectedGroups, func(completed, total int) {
		if progress != nil {
			progress("authors", completed, total, len(posts), fmt.Sprintf("已完成%d/%d位作者，完成后将进行跨作者总归纳", completed, total))
		}
	})

	viewpoints := make([]DailyAuthorView, 0, len(results))
	authors := make([]string, 0, len(results))
	sources := []DailySummarySource{}
	articleCount := 0
	failedAuthors := []string{}
	truncatedAuthors := 0
	for _, result := range results {
		if result.Err != nil {
			failedAuthors = append(failedAuthors, result.Group.Author)
			continue
		}
		viewpoints = append(viewpoints, result.View)
		authors = append(authors, result.View.Author)
		sources = append(sources, result.Sources...)
		articleCount += len(result.Articles)
		if result.View.AvailableArticleCount > result.View.ArticleCount {
			truncatedAuthors++
		}
	}
	if len(viewpoints) == 0 {
		if len(results) > 0 && results[0].Err != nil {
			return DailySummary{}, fmt.Errorf("Hermes 作者观点归纳全部失败: %w", results[0].Err)
		}
		return DailySummary{}, errors.New("Hermes 作者观点归纳未生成有效内容")
	}
	if progress != nil {
		progress("finalizing", len(selectedGroups), len(selectedGroups), articleCount, "作者观点卡已完成，正在生成跨作者共识与明日预期")
	}

	prompt, err := buildDailySummaryPrompt(window, viewpoints)
	if err != nil {
		return DailySummary{}, err
	}
	response, err := a.prompter.Prompt(ctx, prompt)
	if err != nil {
		return DailySummary{}, fmt.Errorf("Hermes 今日观点总归纳失败: %w", err)
	}
	model, err := parseDailySummaryModel(response.Content)
	if err != nil {
		return DailySummary{}, err
	}
	normalizeDailySummaryModel(&model, viewpoints)
	if len(allGroups) > len(selectedGroups) {
		model.Limitations = append(model.Limitations, fmt.Sprintf("有效窗口内共有%d位作者，按最近更新时间选取最多%d位参与总结", len(allGroups), maxDailySummaryAuthors))
	}
	if len(failedAuthors) > 0 {
		model.Limitations = append(model.Limitations, fmt.Sprintf("%d位作者的单作者归纳失败并已跳过：%s", len(failedAuthors), strings.Join(failedAuthors, "、")))
	}
	if truncatedAuthors > 0 {
		model.Limitations = append(model.Limitations, fmt.Sprintf("%d位作者文章较多，作者归纳阶段优先保留较新的文章并按上下文上限截取", truncatedAuthors))
	}
	model.Limitations = cleanStringList(model.Limitations)

	summary := DailySummary{
		TradeDate:             window.TradeDate,
		GeneratedAt:           now.UTC(),
		PromptVersion:         dailySummaryPromptVersion,
		WindowStart:           window.Start.UTC(),
		WindowEnd:             window.End.UTC(),
		FreshnessRule:         window.Rule,
		ArticleCount:          articleCount,
		AuthorCount:           len(authors),
		Authors:               authors,
		Sources:               sources,
		AuthorViews:           viewpoints,
		ExecutiveSummary:      model.ExecutiveSummary,
		MarketRegime:          model.MarketRegime,
		MarketAnalysis:        model.MarketAnalysis,
		MarketFramework:       model.MarketFramework,
		Consensus:             model.Consensus,
		Disagreements:         model.Disagreements,
		Scenarios:             model.Scenarios,
		Directions:            model.Directions,
		TodaySurprises:        model.TodaySurprises,
		TomorrowFocus:         model.TomorrowFocus,
		TomorrowOutlook:       model.TomorrowOutlook,
		TomorrowPlaybook:      model.TomorrowPlaybook,
		Catalysts:             model.Catalysts,
		Risks:                 model.Risks,
		VerificationChecklist: model.VerificationChecklist,
		Limitations:           model.Limitations,
	}
	return a.store.SaveDailySummary(ctx, summary)
}

func effectiveReviewWindow(now time.Time) reviewFreshnessWindow {
	location := shanghaiLocation()
	local := now.In(location)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	window := reviewFreshnessWindow{
		TradeDate: dayStart.Format("2006-01-02"),
		End:       local,
	}
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		window.Start = previousWeekday(dayStart)
		window.Rule = "非交易日仅保留最近一个交易日及其后发布的文章"
		return window
	}
	window.Start = previousWeekday(dayStart)
	marketOpen := dayStart.Add(9*time.Hour + 30*time.Minute)
	if local.Before(marketOpen) {
		window.Rule = "开盘前仅保留上一交易日及今日盘前文章，排除上上个交易日及更早内容"
	} else {
		window.Rule = "开盘后仅保留上一交易日及今日文章，排除上上个交易日及更早内容"
	}
	return window
}

func previousWeekday(dayStart time.Time) time.Time {
	day := dayStart.AddDate(0, 0, -1)
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

func shanghaiLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return location
}

func dailySummaryAuthorGroups(posts []Post) []dailySummaryAuthorGroup {
	groupsByKey := map[string]*dailySummaryAuthorGroup{}
	for _, post := range posts {
		if strings.TrimSpace(firstNonEmpty(post.ContentText, post.Digest, post.AISummary)) == "" {
			continue
		}
		author := firstNonEmpty(strings.TrimSpace(post.AuthorName), sourceName(post.Source))
		identity := strings.TrimSpace(post.AuthorID)
		if identity == "" {
			identity = strings.ToLower(author)
		}
		key := strings.ToLower(strings.TrimSpace(post.Source)) + "\x00" + identity
		group := groupsByKey[key]
		if group == nil {
			group = &dailySummaryAuthorGroup{Key: key, Author: author, Source: post.Source}
			groupsByKey[key] = group
		}
		group.Posts = append(group.Posts, post)
		if post.PublishedAt.After(group.LatestAt) {
			group.LatestAt = post.PublishedAt
		}
	}
	groups := make([]dailySummaryAuthorGroup, 0, len(groupsByKey))
	for _, group := range groupsByKey {
		sort.SliceStable(group.Posts, func(i, j int) bool {
			return group.Posts[i].PublishedAt.After(group.Posts[j].PublishedAt)
		})
		groups = append(groups, *group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if !groups[i].LatestAt.Equal(groups[j].LatestAt) {
			return groups[i].LatestAt.After(groups[j].LatestAt)
		}
		if len(groups[i].Posts) != len(groups[j].Posts) {
			return len(groups[i].Posts) > len(groups[j].Posts)
		}
		return groups[i].Author < groups[j].Author
	})
	return groups
}

func selectDailySummaryAuthorGroups(groups []dailySummaryAuthorGroup) []dailySummaryAuthorGroup {
	return groups[:min(len(groups), maxDailySummaryAuthors)]
}

func (a *Automation) summarizeDailyAuthors(ctx context.Context, groups []dailySummaryAuthorGroup, onProgress func(completed, total int)) []authorSummaryResult {
	results := make([]authorSummaryResult, len(groups))
	jobs := make(chan int, len(groups))
	for index := range groups {
		jobs <- index
	}
	close(jobs)

	workerCount := min(maxAuthorSummaryConcurrency, len(groups))
	var workers sync.WaitGroup
	var progressMu sync.Mutex
	completed := 0
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for index := range jobs {
				group := groups[index]
				view, articles, sources, err := a.summarizeDailyAuthor(ctx, group)
				results[index] = authorSummaryResult{Group: group, View: view, Articles: articles, Sources: sources, Err: err}
				progressMu.Lock()
				completed++
				if onProgress != nil {
					onProgress(completed, len(groups))
				}
				progressMu.Unlock()
			}
		}()
	}
	workers.Wait()
	return results
}

func (a *Automation) summarizeDailyAuthor(ctx context.Context, group dailySummaryAuthorGroup) (DailyAuthorView, []dailySummaryArticle, []DailySummarySource, error) {
	articles, sources := authorSummaryInputs(group)
	if len(articles) == 0 {
		return DailyAuthorView{}, nil, nil, errors.New("作者文章正文不足")
	}
	prompt, err := buildAuthorSummaryPrompt(group.Author, articles)
	if err != nil {
		return DailyAuthorView{}, nil, nil, err
	}
	response, err := a.prompter.Prompt(ctx, prompt)
	if err != nil {
		return DailyAuthorView{}, nil, nil, fmt.Errorf("归纳作者%s: %w", group.Author, err)
	}
	model, err := parseAuthorViewpointModel(response.Content)
	if err != nil {
		return DailyAuthorView{}, nil, nil, fmt.Errorf("归纳作者%s: %w", group.Author, err)
	}
	view := DailyAuthorView{
		Author:                group.Author,
		Source:                sourceName(group.Source),
		ArticleCount:          len(articles),
		AvailableArticleCount: len(group.Posts),
		TimeRange:             authorArticleTimeRange(articles),
		CoreView:              strings.TrimSpace(model.CoreView),
		MarketInterpretation:  strings.TrimSpace(model.MarketInterpretation),
		ViewEvolution:         cleanStringList(model.ViewEvolution),
		Themes:                cleanStringList(model.Themes),
		TodaySurprises:        authorStockViews(group.Author, model.TodaySurprises),
		TomorrowFocus:         authorStockViews(group.Author, model.TomorrowFocus),
		TomorrowOutlook:       strings.TrimSpace(model.TomorrowOutlook),
		Catalysts:             cleanStringList(model.Catalysts),
		Risks:                 cleanStringList(model.Risks),
		Confidence:            strings.TrimSpace(model.Confidence),
		Evidence:              cleanStringList(model.Evidence),
		Sources:               sources,
	}
	return view, articles, sources, nil
}

func authorSummaryInputs(group dailySummaryAuthorGroup) ([]dailySummaryArticle, []DailySummarySource) {
	selected := make([]dailySummaryArticle, 0, min(len(group.Posts), maxAuthorSummaryArticles))
	sources := make([]DailySummarySource, 0, cap(selected))
	remaining := maxAuthorSummaryTotalRunes
	for _, post := range group.Posts {
		if len(selected) >= maxAuthorSummaryArticles || remaining <= 0 {
			break
		}
		content := strings.TrimSpace(firstNonEmpty(post.ContentText, post.Digest, post.AISummary))
		if content == "" {
			continue
		}
		content = truncateRunes(content, min(maxAuthorSummaryArticleRunes, remaining))
		remaining -= len([]rune(content))
		selected = append(selected, dailySummaryArticle{
			Source:      sourceName(post.Source),
			Author:      group.Author,
			Title:       cleanInline(post.Title),
			PublishedAt: post.PublishedAt.In(shanghaiLocation()).Format("2006-01-02 15:04"),
			Content:     content,
		})
		sources = append(sources, DailySummarySource{
			PostID:      post.ID,
			Author:      group.Author,
			Title:       cleanInline(post.Title),
			Source:      post.Source,
			URL:         post.OriginalURL,
			PublishedAt: post.PublishedAt.In(shanghaiLocation()).Format(time.RFC3339),
		})
	}
	// Selection is newest-first so recent corrections win the context budget;
	// present the chosen articles chronologically so the model can see evolution.
	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
		sources[left], sources[right] = sources[right], sources[left]
	}
	for index := range selected {
		selected[index].Index = index + 1
	}
	return selected, sources
}

func buildAuthorSummaryPrompt(author string, articles []dailySummaryArticle) (string, error) {
	data, err := json.Marshal(articles)
	if err != nil {
		return "", fmt.Errorf("整理作者文章: %w", err)
	}
	return `任务阶段：单作者观点归纳。
你是资深A股短线复盘研究员。下面全部文章来自同一位作者，请把该作者在有效时间窗口内的多篇文章压缩为一张“作者观点卡”，供下一阶段跨作者统计使用。

作者：` + author + `

归纳纪律：
1. 输入按发布时间从早到晚排列。后发表文章对早先判断有修正、否定或更新时，以较晚文章的最终观点为准，同时在 view_evolution 简要保留变化。
2. 合并重复内容。作者一天发多篇文章仍然只有一票，不得因重复提及而提高置信度，不得把文章篇数写成共识人数。
3. 只保留仍具时效性的最终判断、已发生的盘面解释、明日预期和可验证条件；已被作者后文否定或已经兑现失效的旧预期不得继续放入 core_view 或 tomorrow_focus。
4. 严格区分盘面事实、作者解释和未来推演。只能使用输入文章，不得补造指数、成交额、涨停数量、公告、股票代码或盘中事实。
5. 今日超预期个股必须有作者明确描述的超预期、弱转强、主动走强、逆势承接、带动板块、容量突破等证据；证据不足返回空数组。
6. 明日关注个股必须写清逻辑、确认条件、失效条件和风险，不得给买卖指令。股票代码只在原文明确提供时填写。
7. evidence 使用“[时间/标题] 观点摘要”的短句，不得伪造原文引语。confidence 只能为“高”“中”“低”，依据作者观点是否完整、一致且有明确验证条件判断。

只返回严格JSON，不要Markdown，不要解释，字段必须完整：
{
  "core_view":"该作者当前最终核心观点，去重后100至250字",
  "market_interpretation":"作者对今日盘面、情绪周期、主线与核心/后排关系的最终解释；未提供则写未明确",
  "view_evolution":["作者在窗口内发生的观点变化或后文修正；没有则返回空数组"],
  "themes":["作者仍然认可或重点观察的题材/方向"],
  "today_surprises":[{"name":"个股名","symbol":"原文明确才填","logic":"为何超预期","evidence":["证据摘要"],"trigger":"今日确认信号","invalidation":"后续失效条件","risk":"风险"}],
  "tomorrow_focus":[{"name":"个股名","symbol":"原文明确才填","logic":"明日关注逻辑","evidence":["证据摘要"],"trigger":"明日确认条件","invalidation":"明日失效条件","risk":"风险"}],
  "tomorrow_outlook":"该作者对下一交易日的基础/偏强/偏弱预期；未明确则写未明确",
  "catalysts":["作者明确提到的催化"],
  "risks":["作者明确提到或可从其观点直接归纳的风险"],
  "confidence":"高/中/低",
  "evidence":["支撑最终观点的时间/标题与摘要，最多8条"]
}

输入文章JSON：` + string(data), nil
}

func parseAuthorViewpointModel(content string) (authorViewpointModel, error) {
	var result authorViewpointModel
	if err := json.Unmarshal([]byte(jsonObject(content)), &result); err != nil {
		return authorViewpointModel{}, fmt.Errorf("Hermes 作者观点归纳未返回有效 JSON: %w", err)
	}
	if strings.TrimSpace(result.CoreView) == "" {
		return authorViewpointModel{}, errors.New("Hermes 作者观点归纳缺少核心观点")
	}
	return result, nil
}

func authorStockViews(author string, items []authorStockViewModel) []DailyStockView {
	result := make([]DailyStockView, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Logic) == "" {
			continue
		}
		result = append(result, DailyStockView{
			Name:         strings.TrimSpace(item.Name),
			Symbol:       strings.TrimSpace(item.Symbol),
			Logic:        strings.TrimSpace(item.Logic),
			SupportCount: 1,
			Authors:      []string{author},
			Evidence:     cleanStringList(item.Evidence),
			Trigger:      strings.TrimSpace(item.Trigger),
			Invalidation: strings.TrimSpace(item.Invalidation),
			Risk:         strings.TrimSpace(item.Risk),
		})
	}
	return result
}

func authorArticleTimeRange(articles []dailySummaryArticle) string {
	if len(articles) == 0 {
		return ""
	}
	if len(articles) == 1 {
		return articles[0].PublishedAt
	}
	return articles[0].PublishedAt + " 至 " + articles[len(articles)-1].PublishedAt
}

func buildDailySummaryPrompt(window reviewFreshnessWindow, viewpoints []DailyAuthorView) (string, error) {
	data, err := json.Marshal(viewpoints)
	if err != nil {
		return "", fmt.Errorf("整理作者观点卡: %w", err)
	}
	prompt := `任务阶段：跨作者总归纳。
你是资深A股短线复盘研究员和“多源观点共识分析器”。输入不是原始文章，而是最多30位作者各自独立归纳后的“作者观点卡”。请生成当日跨作者观点总结。

目标：寻找不同作者之间反复出现的共识、分歧和次日可验证预期。每张作者观点卡最多只算一票；article_count 只是该作者观点卡的原始文章覆盖数，绝不能当成支持票数。

分析纪律：
1. 只能使用输入观点卡提供的信息，不得编造指数涨跌、成交额、涨停数量、公司公告、股票代码或盘中事实。观点卡没有提供的数据要明确写“文章未提供”。
2. 每位作者只算一个独立观点来源，support_count 必须按不同作者计数，不能按文章篇数、重复主题次数或同一作者的多只个股计票。
3. consensus 只收录至少2位不同作者共同支持的观点，优先提炼覆盖作者最多、对次日交易最有解释力的共性。单一作者观点不得伪装成市场共识。
4. 区分三层信息：已发生的盘面事实、作者对事实的解释、面向明日的推演。不要把预测写成事实。
5. “今日超预期个股”必须来自观点卡明确保留的超预期证据；若证据不足返回空数组。
6. “明日预期个股”应优先选择多作者反复提及、具有明确逻辑和可验证条件的个股。每只都要给出逻辑、明日触发条件、失效条件和主要风险；不得直接给买卖指令。
7. market_framework 必须分别回答四个问题：周期处于哪里、资金正在奖励/惩罚什么、主要方向如何竞争、次日应采用何种观察与执行方法。证据不足时明确写“样本不足”，不要强行填满。
8. 对同一题材或个股出现相反看法时，放入 disagreements。positions 要按作者逐项保留立场、观点和证据；不要强行求同，也不要把模型自己的判断伪装成作者观点。
9. scenarios 必须恰好覆盖基础、偏强、偏弱三种情景，key 分别只能为 base、strong、weak；每个情景写清触发、确认、失效和关注点，彼此条件应可区分。
10. directions 按次日研究优先级组织题材或风格方向，stance 只能优先使用“优先观察、等待证明、谨慎追高、事件博弈、回避”之一。每个方向写清支持/反对作者、相关个股、确认条件、失效条件和风险；不得把没有作者依据的方向加入列表。
11. 识别可能的叙事拥挤、幸存者偏差、盘后归因、情绪一致性过高、利好兑现、缩量加速、后排掉队等风险。
12. 明日剧本必须覆盖竞价/盘前、开盘前30分钟、盘中确认和收盘验证，写成可观察条件，而不是确定性预测。
13. verification_checklist 给出5至10条次日可以逐项核对的信号，优先关注主线强弱、核心股反馈、容量/高度/补涨关系、分歧修复、负反馈扩散和量价承接。
14. evidence 使用“[作者] 观点摘要”的短句，不能杜撰原文引语。limitations 要主动说明样本数量、作者同质化、数据缺口和低置信度结论。
15. 语言应简洁、专业、可复盘。避免空泛表述，避免“建议买入/卖出”，避免把大V共识当作事实真相。

只返回严格JSON，不要Markdown，不要解释，字段必须完整：
{
  "executive_summary":"150至300字的今日核心结论，突出最强共识、最大分歧和明日核心变量",
  "market_regime":"用一个短语概括情绪/周期阶段，例如修复、分歧、加速、退潮或混沌；证据不足写样本不足",
  "market_analysis":"今日盘面结构分析，说明主线、支线、情绪、核心与后排关系，以及文章共同认可的驱动",
  "market_framework":{"cycle":"周期位置与依据","capital_pricing":"资金奖励与惩罚的交易特征","direction_competition":"主线、支线和潜在切换关系","trading_method":"与当前周期匹配的观察和执行方法"},
  "consensus":[{"topic":"共识主题","conclusion":"跨作者共同结论","support_count":2,"authors":["作者"],"evidence":["[作者] 观点摘要"]}],
  "disagreements":[{"topic":"分歧主题","views":["观点A","观点B"],"authors":["作者"],"positions":[{"author":"作者","stance":"看强/中性/谨慎等作者真实立场","view":"该作者的具体判断","evidence":"[作者] 证据摘要"}]}],
  "scenarios":[
    {"key":"base","name":"基础情景","summary":"最大概率的路径","trigger":"进入该路径的触发","confirmation":"盘中确认信号","invalidation":"失效条件","focus":["关注方向或变量"]},
    {"key":"strong","name":"偏强情景","summary":"超预期走强的路径","trigger":"进入该路径的触发","confirmation":"盘中确认信号","invalidation":"失效条件","focus":["关注方向或变量"]},
    {"key":"weak","name":"偏弱情景","summary":"负反馈扩散的路径","trigger":"进入该路径的触发","confirmation":"盘中确认信号","invalidation":"失效条件","focus":["关注方向或变量"]}
  ],
  "directions":[{"name":"题材或风格方向","stance":"优先观察/等待证明/谨慎追高/事件博弈/回避","summary":"方向定位与竞争关系","supporting_authors":["作者"],"opposing_authors":["作者"],"stocks":["原观点卡明确提到的个股"],"trigger":"转强或延续确认条件","invalidation":"失效条件","risks":["风险"]}],
  "today_surprises":[{"name":"个股名","symbol":"仅在观点卡明确给出时填写","logic":"为何超预期","support_count":1,"authors":["作者"],"evidence":["[作者] 观点摘要"],"trigger":"今日被确认的信号","invalidation":"后续什么现象会否定该判断","risk":"主要风险"}],
  "tomorrow_focus":[{"name":"个股名","symbol":"仅在观点卡明确给出时填写","logic":"为何进入明日预期","support_count":1,"authors":["作者"],"evidence":["[作者] 观点摘要"],"trigger":"明日需要出现的确认信号","invalidation":"明日失效条件","risk":"主要风险"}],
  "tomorrow_outlook":"按基础情景、偏强情景、偏弱情景描述明日预期和最关键变量",
  "tomorrow_playbook":{"pre_open":["竞价/盘前观察"],"opening":["开盘前30分钟观察"],"intraday":["盘中确认"],"close":["收盘验证"]},
  "catalysts":["观点卡共同提及或可直接归纳的催化；不确定则说明"],
  "risks":["共识交易与盘面的主要风险"],
  "verification_checklist":["明日可逐项核对的信号"],
  "limitations":["样本和结论局限"]
}

统计日期：` + window.TradeDate + `（Asia/Shanghai）
有效文章窗口：` + window.Start.Format("2006-01-02 15:04") + ` 至 ` + window.End.Format("2006-01-02 15:04") + `
时效规则：` + window.Rule + `
输入作者观点卡JSON：` + string(data)
	return prompt, nil
}

func parseDailySummaryModel(content string) (dailySummaryModel, error) {
	var result dailySummaryModel
	if err := json.Unmarshal([]byte(jsonObject(content)), &result); err != nil {
		return dailySummaryModel{}, fmt.Errorf("Hermes 今日观点总结未返回有效 JSON: %w", err)
	}
	if strings.TrimSpace(result.ExecutiveSummary) == "" || strings.TrimSpace(result.TomorrowOutlook) == "" {
		return dailySummaryModel{}, errors.New("Hermes 今日观点总结缺少核心结论或明日预期")
	}
	return result, nil
}

func jsonObject(content string) string {
	content = strings.TrimSpace(content)
	if start, end := strings.Index(content, "{"), strings.LastIndex(content, "}"); start >= 0 && end > start {
		return content[start : end+1]
	}
	return content
}

func normalizeDailySummaryModel(model *dailySummaryModel, viewpoints []DailyAuthorView) {
	knownAuthors := make(map[string]bool, len(viewpoints))
	knownStocks := map[string]bool{}
	knownSymbols := map[string]map[string]bool{}
	for _, viewpoint := range viewpoints {
		knownAuthors[viewpoint.Author] = true
		for _, item := range append(append([]DailyStockView{}, viewpoint.TodaySurprises...), viewpoint.TomorrowFocus...) {
			name := strings.TrimSpace(item.Name)
			if name == "" {
				continue
			}
			knownStocks[name] = true
			symbol := strings.TrimSpace(item.Symbol)
			if symbol != "" {
				if knownSymbols[name] == nil {
					knownSymbols[name] = map[string]bool{}
				}
				knownSymbols[name][symbol] = true
			}
		}
	}
	model.ExecutiveSummary = strings.TrimSpace(model.ExecutiveSummary)
	model.MarketRegime = strings.TrimSpace(model.MarketRegime)
	model.MarketAnalysis = strings.TrimSpace(model.MarketAnalysis)
	model.TomorrowOutlook = strings.TrimSpace(model.TomorrowOutlook)
	model.MarketFramework.Cycle = strings.TrimSpace(model.MarketFramework.Cycle)
	model.MarketFramework.CapitalPricing = strings.TrimSpace(model.MarketFramework.CapitalPricing)
	model.MarketFramework.DirectionCompetition = strings.TrimSpace(model.MarketFramework.DirectionCompetition)
	model.MarketFramework.TradingMethod = strings.TrimSpace(model.MarketFramework.TradingMethod)
	consensus := make([]DailyConsensus, 0, len(model.Consensus))
	for _, item := range model.Consensus {
		item.Topic = strings.TrimSpace(item.Topic)
		item.Conclusion = strings.TrimSpace(item.Conclusion)
		item.Authors = filterKnownAuthors(item.Authors, knownAuthors)
		item.Evidence = cleanStringList(item.Evidence)
		item.SupportCount = len(item.Authors)
		if item.SupportCount >= 2 && strings.TrimSpace(item.Topic) != "" && strings.TrimSpace(item.Conclusion) != "" {
			consensus = append(consensus, item)
		}
	}
	model.Consensus = consensus
	disagreements := make([]DailyDisagreement, 0, len(model.Disagreements))
	for _, item := range model.Disagreements {
		item.Topic = strings.TrimSpace(item.Topic)
		item.Views = cleanStringList(item.Views)
		item.Authors = filterKnownAuthors(item.Authors, knownAuthors)
		positions := make([]DailyDisagreementPosition, 0, len(item.Positions))
		for _, position := range item.Positions {
			position.Author = strings.TrimSpace(position.Author)
			position.Stance = strings.TrimSpace(position.Stance)
			position.View = strings.TrimSpace(position.View)
			position.Evidence = strings.TrimSpace(position.Evidence)
			if !knownAuthors[position.Author] || position.View == "" {
				continue
			}
			positions = append(positions, position)
			item.Authors = append(item.Authors, position.Author)
			item.Views = append(item.Views, position.View)
		}
		item.Positions = positions
		item.Authors = filterKnownAuthors(item.Authors, knownAuthors)
		item.Views = cleanStringList(item.Views)
		if item.Topic != "" && len(item.Views) > 1 && len(item.Authors) > 0 {
			disagreements = append(disagreements, item)
		}
	}
	model.Disagreements = disagreements
	model.Scenarios = normalizeScenarios(model.Scenarios)
	model.Directions = normalizeDirections(model.Directions, knownAuthors, knownStocks)
	model.TodaySurprises = normalizeStockViews(model.TodaySurprises, knownAuthors, knownSymbols)
	model.TomorrowFocus = normalizeStockViews(model.TomorrowFocus, knownAuthors, knownSymbols)
	model.TomorrowPlaybook.PreOpen = cleanStringList(model.TomorrowPlaybook.PreOpen)
	model.TomorrowPlaybook.Opening = cleanStringList(model.TomorrowPlaybook.Opening)
	model.TomorrowPlaybook.Intraday = cleanStringList(model.TomorrowPlaybook.Intraday)
	model.TomorrowPlaybook.Close = cleanStringList(model.TomorrowPlaybook.Close)
	model.Catalysts = cleanStringList(model.Catalysts)
	model.Risks = cleanStringList(model.Risks)
	model.VerificationChecklist = cleanStringList(model.VerificationChecklist)
	model.Limitations = cleanStringList(model.Limitations)
}

func normalizeScenarios(items []DailyScenario) []DailyScenario {
	allowed := map[string]string{"base": "基础情景", "strong": "偏强情景", "weak": "偏弱情景"}
	seen := map[string]bool{}
	result := make([]DailyScenario, 0, 3)
	for _, item := range items {
		item.Key = strings.ToLower(strings.TrimSpace(item.Key))
		defaultName, ok := allowed[item.Key]
		if !ok || seen[item.Key] {
			continue
		}
		item.Name = strings.TrimSpace(item.Name)
		if item.Name == "" {
			item.Name = defaultName
		}
		item.Summary = strings.TrimSpace(item.Summary)
		item.Trigger = strings.TrimSpace(item.Trigger)
		item.Confirmation = strings.TrimSpace(item.Confirmation)
		item.Invalidation = strings.TrimSpace(item.Invalidation)
		item.Focus = cleanStringList(item.Focus)
		if item.Summary == "" {
			continue
		}
		seen[item.Key] = true
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		order := map[string]int{"base": 0, "strong": 1, "weak": 2}
		return order[result[i].Key] < order[result[j].Key]
	})
	return result
}

func normalizeDirections(items []DailyDirectionView, knownAuthors, knownStocks map[string]bool) []DailyDirectionView {
	result := make([]DailyDirectionView, 0, len(items))
	for _, item := range items {
		item.Name = strings.TrimSpace(item.Name)
		item.Stance = strings.TrimSpace(item.Stance)
		item.Summary = strings.TrimSpace(item.Summary)
		item.SupportingAuthors = filterKnownAuthors(item.SupportingAuthors, knownAuthors)
		item.OpposingAuthors = filterKnownAuthors(item.OpposingAuthors, knownAuthors)
		item.Stocks = filterKnownValues(item.Stocks, knownStocks)
		item.Trigger = strings.TrimSpace(item.Trigger)
		item.Invalidation = strings.TrimSpace(item.Invalidation)
		item.Risks = cleanStringList(item.Risks)
		if item.Name != "" && item.Summary != "" && (len(item.SupportingAuthors) > 0 || len(item.OpposingAuthors) > 0) {
			result = append(result, item)
		}
	}
	return result
}

func normalizeStockViews(items []DailyStockView, knownAuthors map[string]bool, knownSymbols map[string]map[string]bool) []DailyStockView {
	result := make([]DailyStockView, 0, len(items))
	for _, item := range items {
		item.Name = strings.TrimSpace(item.Name)
		item.Symbol = strings.TrimSpace(item.Symbol)
		item.Logic = strings.TrimSpace(item.Logic)
		item.Trigger = strings.TrimSpace(item.Trigger)
		item.Invalidation = strings.TrimSpace(item.Invalidation)
		item.Risk = strings.TrimSpace(item.Risk)
		item.Authors = filterKnownAuthors(item.Authors, knownAuthors)
		item.Evidence = cleanStringList(item.Evidence)
		item.SupportCount = len(item.Authors)
		if item.Symbol != "" && !knownSymbols[item.Name][item.Symbol] {
			item.Symbol = ""
		}
		if item.Name != "" && item.Logic != "" && item.SupportCount > 0 {
			result = append(result, item)
		}
	}
	return result
}

func filterKnownValues(values []string, known map[string]bool) []string {
	filtered := []string{}
	for _, value := range cleanStringList(values) {
		if known[value] {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func filterKnownAuthors(values []string, known map[string]bool) []string {
	filtered := []string{}
	for _, value := range cleanStringList(values) {
		if known[value] {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func cleanStringList(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
