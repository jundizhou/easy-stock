package methodology

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	defaultTreeURL    = "https://api.github.com/repos/zhouqinglong520/trading-mastery/git/trees/main?recursive=1"
	defaultRawBaseURL = "https://raw.githubusercontent.com/zhouqinglong520/trading-mastery/main/"
	defaultSourceURL  = "https://github.com/zhouqinglong520/trading-mastery/tree/main/%E6%B8%B8%E8%B5%84%E5%BF%83%E6%B3%95"
	manifestVersion   = 4
	skillName         = "a-stock-short-term-masters"
)

type Config struct {
	CacheDir        string
	HermesHome      string
	HTTPClient      *http.Client
	TreeURL         string
	RawBaseURL      string
	SourceURL       string
	RefreshInterval time.Duration
	BuiltinFS       fs.FS
	DisableBuiltin  bool
}

type Library struct {
	config Config
	mu     sync.Mutex
	cache  *manifest
}

type Snapshot struct {
	Traders          []TraderSummary `json:"traders"`
	FetchedAt        time.Time       `json:"fetched_at"`
	SourceURL        string          `json:"source_url"`
	SourceCommit     string          `json:"source_commit,omitempty"`
	Stale            bool            `json:"stale"`
	KnowledgeStatus  string          `json:"knowledge_status"`
	KnowledgeMessage string          `json:"knowledge_message,omitempty"`
}

type TraderSummary struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	DocumentCount    int      `json:"document_count"`
	CharacterCount   int      `json:"character_count"`
	ReadingMinutes   int      `json:"reading_minutes"`
	PlaceholderCount int      `json:"placeholder_count"`
	Tags             []string `json:"tags,omitempty"`
	Quote            string   `json:"quote,omitempty"`
	SourceURL        string   `json:"source_url"`
}

type TraderDetail struct {
	TraderSummary
	Documents []Document `json:"documents"`
}

type Document struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Kind             string   `json:"kind"`
	Content          string   `json:"content"`
	SourceURL        string   `json:"source_url"`
	CharacterCount   int      `json:"character_count"`
	PlaceholderCount int      `json:"placeholder_count"`
	Tags             []string `json:"tags,omitempty"`
}

type manifest struct {
	Version   int              `json:"version"`
	FetchedAt time.Time        `json:"fetched_at"`
	Commit    string           `json:"commit"`
	SourceURL string           `json:"source_url"`
	Documents []cachedDocument `json:"documents"`
}

type cachedDocument struct {
	ID               string   `json:"id"`
	TraderID         string   `json:"trader_id"`
	TraderName       string   `json:"trader_name"`
	Title            string   `json:"title"`
	Kind             string   `json:"kind"`
	RelativePath     string   `json:"relative_path"`
	SourceURL        string   `json:"source_url"`
	CacheFile        string   `json:"cache_file"`
	CharacterCount   int      `json:"character_count"`
	PlaceholderCount int      `json:"placeholder_count"`
	Tags             []string `json:"tags,omitempty"`
	Quote            string   `json:"quote,omitempty"`
}

type githubTree struct {
	SHA       string `json:"sha"`
	Truncated bool   `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
}

func NewLibrary(cfg Config) *Library {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if strings.TrimSpace(cfg.TreeURL) == "" {
		cfg.TreeURL = defaultTreeURL
	}
	if strings.TrimSpace(cfg.RawBaseURL) == "" {
		cfg.RawBaseURL = defaultRawBaseURL
	}
	if strings.TrimSpace(cfg.SourceURL) == "" {
		cfg.SourceURL = defaultSourceURL
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 24 * time.Hour
	}
	if cfg.BuiltinFS == nil && !cfg.DisableBuiltin {
		cfg.BuiltinFS = builtinMasteryFS
	}
	return &Library{config: cfg}
}

func (l *Library) Snapshot(ctx context.Context, force bool) (Snapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	current, _ := l.loadManifest()
	if (current == nil || !l.manifestFilesReady(current)) && l.config.BuiltinFS != nil {
		if seeded, seedErr := l.seedBuiltinCache(); seedErr == nil {
			current = seeded
		}
	}
	if current != nil && !force && time.Since(current.FetchedAt) < l.config.RefreshInterval && l.manifestFilesReady(current) {
		l.cache = current
		return l.snapshotFromManifest(current, false)
	}

	refreshed, err := l.refresh(ctx, current)
	if err != nil {
		if current == nil || !l.manifestFilesReady(current) {
			return Snapshot{}, err
		}
		l.cache = current
		snapshot, snapshotErr := l.snapshotFromManifest(current, true)
		if snapshotErr != nil {
			return Snapshot{}, err
		}
		snapshot.KnowledgeMessage = firstNonEmpty(snapshot.KnowledgeMessage, "远程更新失败，正在使用本地缓存："+err.Error())
		return snapshot, nil
	}
	l.cache = refreshed
	return l.snapshotFromManifest(refreshed, false)
}

func (l *Library) seedBuiltinCache() (*manifest, error) {
	if l.config.BuiltinFS == nil {
		return nil, errors.New("内置游资心法资料未配置")
	}
	content, err := fs.ReadFile(l.config.BuiltinFS, "builtin/manifest.json")
	if err != nil {
		return nil, fmt.Errorf("读取内置游资心法清单: %w", err)
	}
	var current manifest
	if err := json.Unmarshal(content, &current); err != nil {
		return nil, fmt.Errorf("解析内置游资心法清单: %w", err)
	}
	if len(current.Documents) == 0 {
		return nil, errors.New("内置游资心法资料为空")
	}
	current.Version = manifestVersion
	current.FetchedAt = time.Now()
	current.SourceURL = firstNonEmpty(current.SourceURL, l.config.SourceURL)
	for _, item := range current.Documents {
		cacheFile, err := safeBuiltinCachePath(item.CacheFile)
		if err != nil {
			return nil, fmt.Errorf("内置游资心法文件路径无效 %q: %w", item.CacheFile, err)
		}
		document, err := fs.ReadFile(l.config.BuiltinFS, path.Join("builtin", cacheFile))
		if err != nil {
			return nil, fmt.Errorf("读取内置游资心法 %s: %w", item.TraderName, err)
		}
		if len(strings.TrimSpace(string(document))) < 20 {
			return nil, fmt.Errorf("内置游资心法 %s 内容为空", item.TraderName)
		}
		if err := atomicWrite(filepath.Join(l.config.CacheDir, filepath.FromSlash(cacheFile)), document, 0o644); err != nil {
			return nil, fmt.Errorf("初始化内置游资心法 %s: %w", item.TraderName, err)
		}
	}
	if err := l.saveManifest(&current); err != nil {
		return nil, fmt.Errorf("保存内置游资心法清单: %w", err)
	}
	return &current, nil
}

func safeBuiltinCachePath(value string) (string, error) {
	cleaned := path.Clean(strings.TrimSpace(filepath.ToSlash(value)))
	if cleaned == "" || cleaned == "." || path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", errors.New("路径必须位于缓存目录内")
	}
	return cleaned, nil
}

func (l *Library) Trader(ctx context.Context, id string) (TraderDetail, error) {
	if _, err := l.Snapshot(ctx, false); err != nil {
		return TraderDetail{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cache == nil {
		return TraderDetail{}, errors.New("游资心法缓存尚未就绪")
	}
	summary, ok := l.summaryByID(l.cache, id)
	if !ok {
		return TraderDetail{}, os.ErrNotExist
	}
	detail := TraderDetail{TraderSummary: summary}
	for _, item := range l.cache.Documents {
		if item.TraderID != id {
			continue
		}
		content, err := os.ReadFile(filepath.Join(l.config.CacheDir, item.CacheFile))
		if err != nil {
			return TraderDetail{}, fmt.Errorf("读取游资心法缓存: %w", err)
		}
		detail.Documents = append(detail.Documents, Document{
			ID:               item.ID,
			Title:            item.Title,
			Kind:             item.Kind,
			Content:          string(content),
			SourceURL:        item.SourceURL,
			CharacterCount:   item.CharacterCount,
			PlaceholderCount: item.PlaceholderCount,
			Tags:             append([]string(nil), item.Tags...),
		})
	}
	sort.SliceStable(detail.Documents, func(i, j int) bool {
		return documentKindRank(detail.Documents[i].Kind) < documentKindRank(detail.Documents[j].Kind)
	})
	return detail, nil
}

func (l *Library) ContextForPrompt(ctx context.Context, prompt string, maxChars int) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || !looksLikeMasteryQuestion(prompt) {
		return "", nil
	}
	if maxChars <= 0 {
		maxChars = 12_000
	}
	if _, err := l.Snapshot(ctx, false); err != nil {
		return "", err
	}

	l.mu.Lock()
	current := l.cache
	l.mu.Unlock()
	if current == nil {
		return "", nil
	}

	type candidate struct {
		doc     cachedDocument
		content string
		score   int
	}
	candidates := make([]candidate, 0, len(current.Documents))
	matchedTrader := false
	lowerPrompt := strings.ToLower(prompt)
	terms := searchTerms(prompt)
	for _, item := range current.Documents {
		contentBytes, err := os.ReadFile(filepath.Join(l.config.CacheDir, item.CacheFile))
		if err != nil {
			continue
		}
		content := string(contentBytes)
		score := 0
		if strings.Contains(lowerPrompt, strings.ToLower(item.TraderName)) {
			score += 1000
			matchedTrader = true
		}
		if item.Kind == "deep_report" {
			score += 18
		}
		lowerContent := strings.ToLower(content)
		for _, term := range terms {
			if strings.Contains(lowerContent, strings.ToLower(term)) {
				score += len([]rune(term))
			}
		}
		candidates = append(candidates, candidate{doc: item, content: content, score: score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return documentKindRank(candidates[i].doc.Kind) < documentKindRank(candidates[j].doc.Kind)
	})

	var result strings.Builder
	result.WriteString("以下内容来自本机缓存的《交易大师 · 游资心法库》，属于历史交易经验与二次整理材料，不是实时行情、确定事实或收益承诺。回答时应注明来源并保持批判性。\n\n")
	selected := 0
	seenTraders := map[string]bool{}
	for _, item := range candidates {
		if item.score <= 0 && selected > 0 {
			continue
		}
		if !matchedTrader && seenTraders[item.doc.TraderID] {
			continue
		}
		header := fmt.Sprintf("## %s · %s\n来源：%s\n\n", item.doc.TraderName, item.doc.Title, item.doc.SourceURL)
		remaining := maxChars - result.Len() - len(header)
		if remaining < 500 {
			break
		}
		content := item.content
		if len(content) > remaining {
			content = truncateUTF8(content, remaining) + "\n\n[内容因上下文长度截断，可通过 Hermes 技能继续读取完整原文]"
		}
		result.WriteString(header)
		result.WriteString(content)
		result.WriteString("\n\n")
		selected++
		seenTraders[item.doc.TraderID] = true
		if (!matchedTrader && selected >= 3) || selected >= 5 || result.Len() >= maxChars {
			break
		}
	}
	if selected == 0 {
		return "", nil
	}
	return strings.TrimSpace(result.String()), nil
}

func (l *Library) refresh(ctx context.Context, previous *manifest) (*manifest, error) {
	if strings.TrimSpace(l.config.CacheDir) == "" {
		return nil, errors.New("游资心法缓存目录未配置")
	}
	if err := os.MkdirAll(filepath.Join(l.config.CacheDir, "documents"), 0o755); err != nil {
		return nil, fmt.Errorf("创建游资心法缓存目录: %w", err)
	}
	tree, err := l.fetchTree(ctx)
	if err != nil {
		return nil, err
	}
	if tree.Truncated {
		return nil, errors.New("GitHub 返回的游资心法目录不完整")
	}
	if previous != nil && previous.Commit == tree.SHA && l.manifestFilesReady(previous) {
		previous.FetchedAt = time.Now()
		if err := l.saveManifest(previous); err != nil {
			return nil, err
		}
		return previous, nil
	}

	documents := make([]cachedDocument, 0, 48)
	for _, entry := range tree.Tree {
		if entry.Type != "blob" || !strings.HasPrefix(entry.Path, "游资心法/") || !strings.HasSuffix(strings.ToLower(entry.Path), ".md") {
			continue
		}
		parts := strings.Split(entry.Path, "/")
		if len(parts) != 3 || strings.TrimSpace(parts[1]) == "" {
			continue
		}
		traderName := strings.TrimSpace(parts[1])
		title := strings.TrimSuffix(parts[2], filepath.Ext(parts[2]))
		documentID := stableID(entry.Path)
		documents = append(documents, cachedDocument{
			ID:           documentID,
			TraderID:     traderID(traderName),
			TraderName:   traderName,
			Title:        title,
			Kind:         documentKind(title),
			RelativePath: entry.Path,
			SourceURL:    sourceFileURL(entry.Path),
			CacheFile:    filepath.Join("documents", documentID+".md"),
		})
	}
	if len(documents) == 0 {
		return nil, errors.New("GitHub 目录中没有找到游资心法 Markdown")
	}
	sort.Slice(documents, func(i, j int) bool {
		if documents[i].TraderName != documents[j].TraderName {
			return documents[i].TraderName < documents[j].TraderName
		}
		return documentKindRank(documents[i].Kind) < documentKindRank(documents[j].Kind)
	})

	if err := l.downloadDocuments(ctx, documents); err != nil {
		return nil, err
	}
	for index := range documents {
		content, err := os.ReadFile(filepath.Join(l.config.CacheDir, documents[index].CacheFile))
		if err != nil {
			return nil, err
		}
		text := string(content)
		documents[index].CharacterCount = len([]rune(text))
		documents[index].PlaceholderCount = strings.Count(text, "待补充") + strings.Count(text, "待从PDF")
		documents[index].Tags = deriveTags(text)
		documents[index].Quote = extractQuote(text)
	}

	next := &manifest{
		Version:   manifestVersion,
		FetchedAt: time.Now(),
		Commit:    tree.SHA,
		SourceURL: l.config.SourceURL,
		Documents: documents,
	}
	if err := l.saveManifest(next); err != nil {
		return nil, err
	}
	return next, nil
}

func (l *Library) fetchTree(ctx context.Context) (githubTree, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, l.config.TreeURL, nil)
	if err != nil {
		return githubTree{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "easy-stock")
	response, err := l.config.HTTPClient.Do(request)
	if err != nil {
		return githubTree{}, fmt.Errorf("读取 GitHub 游资心法目录: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return githubTree{}, fmt.Errorf("读取 GitHub 游资心法目录返回 HTTP %d", response.StatusCode)
	}
	var tree githubTree
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&tree); err != nil {
		return githubTree{}, fmt.Errorf("解析 GitHub 游资心法目录: %w", err)
	}
	return tree, nil
}

func (l *Library) downloadDocuments(ctx context.Context, documents []cachedDocument) error {
	semaphore := make(chan struct{}, 6)
	errorsByPath := make(chan error, len(documents))
	var wait sync.WaitGroup
	for _, item := range documents {
		item := item
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				errorsByPath <- ctx.Err()
				return
			}
			defer func() { <-semaphore }()
			if err := l.downloadDocument(ctx, item); err != nil {
				errorsByPath <- err
			}
		}()
	}
	wait.Wait()
	close(errorsByPath)
	for err := range errorsByPath {
		if err != nil {
			return err
		}
	}
	return nil
}

func (l *Library) downloadDocument(ctx context.Context, item cachedDocument) error {
	rawURL, err := joinEscapedURL(l.config.RawBaseURL, item.RelativePath)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "easy-stock")
	response, err := l.config.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("下载 %s: %w", item.RelativePath, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("下载 %s 返回 HTTP %d", item.RelativePath, response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("下载 %s: %w", item.RelativePath, err)
	}
	if len(strings.TrimSpace(string(content))) < 20 {
		return fmt.Errorf("下载 %s 得到空内容", item.RelativePath)
	}
	return atomicWrite(filepath.Join(l.config.CacheDir, item.CacheFile), content, 0o644)
}

func (l *Library) snapshotFromManifest(current *manifest, stale bool) (Snapshot, error) {
	snapshot := Snapshot{
		Traders:         l.summaries(current),
		FetchedAt:       current.FetchedAt,
		SourceURL:       firstNonEmpty(current.SourceURL, l.config.SourceURL),
		SourceCommit:    current.Commit,
		Stale:           stale,
		KnowledgeStatus: "ready",
	}
	if err := l.syncHermesKnowledge(current); err != nil {
		snapshot.KnowledgeStatus = "limited"
		snapshot.KnowledgeMessage = "页面资料可用，但 Hermes 知识同步失败：" + err.Error()
	}
	return snapshot, nil
}

func (l *Library) summaries(current *manifest) []TraderSummary {
	byID := map[string]*TraderSummary{}
	for _, item := range current.Documents {
		summary := byID[item.TraderID]
		if summary == nil {
			summary = &TraderSummary{
				ID:        item.TraderID,
				Name:      item.TraderName,
				SourceURL: traderSourceURL(item.TraderName),
			}
			byID[item.TraderID] = summary
		}
		summary.DocumentCount++
		summary.CharacterCount += item.CharacterCount
		summary.PlaceholderCount += item.PlaceholderCount
		if summary.Quote == "" && item.Quote != "" {
			summary.Quote = item.Quote
		}
		summary.Tags = mergeStrings(summary.Tags, item.Tags)
	}
	result := make([]TraderSummary, 0, len(byID))
	for _, summary := range byID {
		summary.ReadingMinutes = max(1, summary.CharacterCount/700)
		if len(summary.Tags) > 5 {
			summary.Tags = summary.Tags[:5]
		}
		result = append(result, *summary)
	}
	sort.Slice(result, func(i, j int) bool {
		return masteryPriority(result[i].Name) < masteryPriority(result[j].Name)
	})
	return result
}

func (l *Library) summaryByID(current *manifest, id string) (TraderSummary, bool) {
	for _, summary := range l.summaries(current) {
		if summary.ID == id {
			return summary, true
		}
	}
	return TraderSummary{}, false
}

func (l *Library) syncHermesKnowledge(current *manifest) error {
	if strings.TrimSpace(l.config.HermesHome) == "" {
		return errors.New("Hermes Home 未配置")
	}
	skillDir := filepath.Join(l.config.HermesHome, "skills", "trading", skillName)
	referencesDir := filepath.Join(skillDir, "references")
	if err := os.MkdirAll(referencesDir, 0o700); err != nil {
		return err
	}

	var index strings.Builder
	index.WriteString("---\nname: a-stock-short-term-masters\ndescription: A股超短交易游资心法资料库。用户询问游资、心法、情绪周期、龙头战法、首板、打板、仓位、预期差，或点名炒股养家、92科比、ASKING等人物时必须使用。\n---\n\n")
	index.WriteString("# A股游资心法资料库\n\n资料来自 [trading-mastery 游资心法](" + l.config.SourceURL + ")，由 easy-stock 每日缓存。它是历史经验和二次整理材料，不是事实核验、实时行情或收益承诺。回答时应区分原文观点、你的推断和风险。\n\n")
	index.WriteString("先根据人物索引加载对应 references 文件；比较多位游资时分别加载，不要只依靠模型记忆。深度研读报告通常比学习笔记完整，但仍需指出其中可能存在的占位、概括和事后解释。\n\n## 人物索引\n\n")

	for _, summary := range l.summaries(current) {
		fileName := summary.ID + ".md"
		fmt.Fprintf(&index, "- %s：`references/%s`", summary.Name, fileName)
		if len(summary.Tags) > 0 {
			fmt.Fprintf(&index, "（%s）", strings.Join(summary.Tags, "、"))
		}
		index.WriteString("\n")
		var reference strings.Builder
		fmt.Fprintf(&reference, "# %s\n\n来源：%s\n\n", summary.Name, summary.SourceURL)
		for _, item := range current.Documents {
			if item.TraderID != summary.ID {
				continue
			}
			content, err := os.ReadFile(filepath.Join(l.config.CacheDir, item.CacheFile))
			if err != nil {
				return err
			}
			fmt.Fprintf(&reference, "\n\n---\n\n## %s\n\n原文：%s\n\n%s", item.Title, item.SourceURL, content)
		}
		if err := atomicWrite(filepath.Join(referencesDir, fileName), []byte(reference.String()), 0o600); err != nil {
			return err
		}
	}
	if err := atomicWrite(filepath.Join(skillDir, "SKILL.md"), []byte(index.String()), 0o600); err != nil {
		return err
	}
	return l.syncMemoryIndex(len(l.summaries(current)), current.FetchedAt)
}

func (l *Library) syncMemoryIndex(traderCount int, fetchedAt time.Time) error {
	memoryDir := filepath.Join(l.config.HermesHome, "memories")
	if err := os.MkdirAll(memoryDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(memoryDir, "MEMORY.md")
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	const start = "<!-- easy-stock:short-term-masters:start -->"
	const end = "<!-- easy-stock:short-term-masters:end -->"
	block := fmt.Sprintf("%s\n§\neasy-stock 已在本机安装 `%s` Hermes 技能，包含 %d 位游资的心法原文缓存；涉及游资、情绪周期、龙头、首板、打板、仓位或预期差的问题可加载该技能核对原文。资料最近同步于 %s，属于历史经验材料而非收益承诺。\n%s", start, skillName, traderCount, fetchedAt.Local().Format("2006-01-02 15:04"), end)
	updated := upsertManagedBlock(string(existing), start, end, block)
	return atomicWrite(path, []byte(updated), 0o600)
}

func (l *Library) loadManifest() (*manifest, error) {
	content, err := os.ReadFile(filepath.Join(l.config.CacheDir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var current manifest
	if err := json.Unmarshal(content, &current); err != nil {
		return nil, err
	}
	if current.Version != manifestVersion || len(current.Documents) == 0 {
		return nil, errors.New("游资心法缓存版本无效")
	}
	return &current, nil
}

func (l *Library) saveManifest(current *manifest) error {
	content, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(l.config.CacheDir, "manifest.json"), content, 0o644)
}

func (l *Library) manifestFilesReady(current *manifest) bool {
	if current == nil || len(current.Documents) == 0 {
		return false
	}
	for _, item := range current.Documents {
		info, err := os.Stat(filepath.Join(l.config.CacheDir, item.CacheFile))
		if err != nil || info.IsDir() || info.Size() < 20 {
			return false
		}
	}
	return true
}

func atomicWrite(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, content, mode); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func documentKind(title string) string {
	if strings.Contains(title, "深度研读") {
		return "deep_report"
	}
	if strings.Contains(title, "学习笔记") {
		return "study_notes"
	}
	return "article"
}

func documentKindRank(kind string) int {
	switch kind {
	case "deep_report":
		return 0
	case "study_notes":
		return 1
	default:
		return 2
	}
}

func stableID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:10])
}

func traderID(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

func sourceFileURL(relativePath string) string {
	return "https://github.com/zhouqinglong520/trading-mastery/blob/main/" + escapePath(relativePath)
}

func traderSourceURL(name string) string {
	return defaultSourceURL + "/" + url.PathEscape(name)
}

func escapePath(value string) string {
	parts := strings.Split(value, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

func joinEscapedURL(baseURL, relativePath string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + relativePath
	return base.String(), nil
}

func deriveTags(content string) []string {
	keywords := []string{"情绪周期", "龙头战法", "首板", "打板", "低吸", "预期差", "仓位管理", "风险控制", "弱市", "复盘", "题材", "心态", "趋势", "超预期"}
	result := make([]string, 0, 5)
	for _, keyword := range keywords {
		if strings.Contains(content, keyword) {
			result = append(result, keyword)
		}
		if len(result) >= 5 {
			break
		}
	}
	return result
}

func extractQuote(content string) string {
	for _, original := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(original)
		if !strings.HasPrefix(trimmed, ">") {
			continue
		}
		line := strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
		if line == "" || isQuoteMetadata(line) {
			continue
		}
		line = strings.Trim(line, "*\"“” ")
		if len([]rune(line)) >= 8 && len([]rune(line)) <= 90 {
			return line
		}
	}
	return ""
}

func isQuoteMetadata(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "**") && (strings.Contains(trimmed, "**：") || strings.Contains(trimmed, "**:")) {
		return true
	}
	for _, marker := range []string{"学习目标", "研读目标", "研读方法", "研读说明", "报告说明", "研读时间", "完成标准", "核心收获", "实践要求", "预计"} {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

func looksLikeMasteryQuestion(prompt string) bool {
	lower := strings.ToLower(prompt)
	for _, keyword := range []string{"游资", "心法", "交易大师", "炒股养家", "asking", "92科比", "赵老哥", "退学炒股", "职业炒手", "著名刺客", "瑞鹤仙", "龙飞虎", "乔帮主", "小鳄鱼", "欢乐海岸"} {
		if strings.Contains(lower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func searchTerms(prompt string) []string {
	terms := []string{}
	for _, keyword := range []string{"情绪周期", "龙头", "首板", "打板", "低吸", "半路", "预期差", "仓位", "风控", "回撤", "弱市", "复盘", "题材", "心态", "超预期", "卖点", "买点"} {
		if strings.Contains(prompt, keyword) {
			terms = append(terms, keyword)
		}
	}
	var word strings.Builder
	flush := func() {
		value := strings.TrimSpace(word.String())
		if len([]rune(value)) >= 2 {
			terms = append(terms, value)
		}
		word.Reset()
	}
	for _, char := range prompt {
		if unicode.IsLetter(char) || unicode.IsNumber(char) {
			word.WriteRune(char)
		} else {
			flush()
		}
	}
	flush()
	return mergeStrings(nil, terms)
}

func masteryPriority(name string) string {
	priorities := map[string]int{
		"炒股养家": 1, "ASKING": 2, "职业炒手": 3, "赵老哥": 4, "退学炒股": 5,
		"92科比": 6, "著名刺客": 7, "徐翔": 8, "瑞鹤仙": 9, "龙飞虎": 10,
	}
	if priority, ok := priorities[name]; ok {
		return fmt.Sprintf("%03d", priority)
	}
	return "999" + strings.ToLower(name)
}

func mergeStrings(current []string, values []string) []string {
	seen := make(map[string]bool, len(current)+len(values))
	result := make([]string, 0, len(current)+len(values))
	for _, value := range append(append([]string(nil), current...), values...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func upsertManagedBlock(existing, start, end, block string) string {
	startIndex := strings.Index(existing, start)
	if startIndex >= 0 {
		endIndex := strings.Index(existing[startIndex:], end)
		if endIndex >= 0 {
			endIndex += startIndex + len(end)
			return strings.TrimSpace(existing[:startIndex]+block+existing[endIndex:]) + "\n"
		}
	}
	if strings.TrimSpace(existing) == "" {
		return block + "\n"
	}
	return strings.TrimSpace(existing) + "\n\n" + block + "\n"
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	runes := []rune(value)
	low, high := 0, len(runes)
	for low < high {
		middle := (low + high + 1) / 2
		if len(string(runes[:middle])) <= maxBytes {
			low = middle
		} else {
			high = middle - 1
		}
	}
	return string(runes[:low])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
