export type BackendConfig = {
  backendUrl: string;
  token: string;
};

export type BackendBridge = {
  getBackendConfig: () => Promise<BackendConfig>;
  getWechatServiceStatus?: () => Promise<WechatServiceStatus>;
  openWechatLogin?: () => Promise<WechatServiceStatus>;
  getBrowserAuthStatus?: (profileId: string, source?: 'xueqiu' | 'taoguba') => Promise<BrowserAuthStatus>;
  openReviewSourceLogin?: (source: 'xueqiu' | 'taoguba', profileId: string, homepageURL: string) => Promise<BrowserAuthStatus>;
  openXueqiuLogin?: (profileId: string, homepageURL: string) => Promise<BrowserAuthStatus>;
  getUpdateStatus?: () => Promise<AppUpdateStatus>;
  checkForUpdates?: () => Promise<AppUpdateStatus>;
  downloadUpdate?: () => Promise<AppUpdateStatus>;
  installUpdate?: () => Promise<AppUpdateStatus>;
  openUpdateRelease?: () => Promise<void>;
  openUpdateBackups?: () => Promise<void>;
  onUpdateStatus?: (listener: (status: AppUpdateStatus) => void) => () => void;
};

export type AppUpdateState = 'disabled' | 'idle' | 'checking' | 'available' | 'downloading' | 'downloaded' | 'not-available' | 'error' | 'installing';

export type AppUpdateStatus = {
	state: AppUpdateState;
	supported: boolean;
	installMode?: 'automatic' | 'manual';
	currentVersion: string;
	latestVersion?: string;
	releaseName?: string;
	releaseNotes?: string;
	message: string;
	progress: number;
	transferred?: number;
	total?: number;
	bytesPerSecond?: number;
	backupPath?: string;
	backupCreatedAt?: string;
};

export type WechatServiceStatus = {
	available: boolean;
	configured: boolean;
	authenticated: boolean;
	state: 'starting' | 'not_logged_in' | 'authenticated' | 'expired' | 'error' | string;
	account?: string;
	fakeid?: string;
	expires_at?: string;
	message: string;
	login_url?: string;
};

export type BrowserAuthStatus = {
	configured: boolean;
	updated_at?: string;
	message: string;
};

export type SecretSettingStatus = {
	configured: boolean;
	masked?: string;
};

export type AppSettings = {
	hermes: {
		available: boolean;
		configured: boolean;
		api_key_configured: boolean;
		version?: string;
		message?: string;
	};
	llm: {
		provider: string;
		base_url: string;
		model: string;
		api_mode: 'chat_completions' | 'responses' | 'anthropic_messages' | string;
		response_timeout_seconds: number;
		api_key: SecretSettingStatus;
	};
	llm_profiles: LLMProfile[];
	active_llm_profile_id: string;
	credentials: {
		tushare_token: SecretSettingStatus;
		ths_cookie: SecretSettingStatus;
		xueqiu_cookie: SecretSettingStatus;
		eastmoney_cookie: SecretSettingStatus;
		wechat_api_token: SecretSettingStatus;
	};
	review_automation: {
		profiles: ReviewAutomationProfile[];
	};
	updated_at?: string;
};

export type LLMProfile = {
	id: string;
	name: string;
	provider: string;
	base_url: string;
	model: string;
	api_mode: 'chat_completions' | 'responses' | 'codex_responses' | 'anthropic_messages' | string;
	api_key: SecretSettingStatus;
};

export type HermesSkillSetting = {
	name: string;
	description: string;
	category: string;
	enabled: boolean;
};

export type HermesMCPServerSetting = {
	name: string;
	enabled: boolean;
	transport: 'stdio' | 'http' | 'sse';
	command?: string;
	args?: string[];
	env?: Record<string, SecretSettingStatus>;
	url?: string;
	headers?: Record<string, SecretSettingStatus>;
	timeout?: number;
	connect_timeout?: number;
	supports_parallel_tool_calls?: boolean;
};

export type HermesAgentSettings = {
  reasoning_effort: 'none' | 'minimal' | 'low' | 'medium' | 'high' | 'xhigh' | 'max' | string;
  skills: HermesSkillSetting[];
  mcp_servers: HermesMCPServerSetting[];
};

export type LLMConnectionTestResult = {
	ok: boolean;
	provider: string;
	model: string;
	api_mode: string;
	runtime: 'hermes' | string;
	latency_ms: number;
	response: string;
};

export type LLMModelOption = {
	id: string;
	owned_by?: string;
	display_name?: string;
};

export type LLMModelsResult = {
	models: LLMModelOption[];
	source_url: string;
};

export type ReviewAutomationProfile = {
	id: string;
	source: 'wechat' | 'xueqiu' | 'taoguba' | string;
	name: string;
	base_url: string;
	credential: SecretSettingStatus;
	sync_hour: number;
	auto_analyze: boolean;
	enabled: boolean;
};

export type ResolveInput = {
  bridge?: BackendBridge;
  env?: Record<string, string | undefined>;
};

export type SourceHealth = {
  id: string;
  name: string;
  category: string;
  ok: boolean;
  message?: string;
  checked_at: string;
};

export type SourceMeta = {
  source: string;
  source_url?: string;
  available_fields?: string[];
  fetched_at: string;
  latency_ms: number;
  stale: boolean;
  trade_date?: string;
  snapshot_id?: string;
  next_refresh_at?: string;
  fallback_reason?: string;
  carry_forward?: boolean;
};

export type Quote = {
  symbol: string;
  name: string;
  price: number;
  open: number;
  previous_close: number;
  high: number;
  low: number;
  change: number;
  change_percent: number;
  trade_time?: string;
  meta: SourceMeta;
};

export type KLine = {
  symbol: string;
  time: string;
  open: number;
  high: number;
  low: number;
  close: number;
  previous_close?: number;
  volume: number;
  amount: number;
  turnover_rate?: number;
  change_percent?: number;
  meta: SourceMeta;
};

export type MarketIndexSnapshot = {
	id: string;
	secid: string;
	code: string;
	name: string;
	region: string;
	market: string;
	currency: string;
	price: number;
	change: number;
	change_percent: number;
	trade_time?: string;
	status: string;
	meta: SourceMeta;
};

export type MarketIndexSeries = {
	index: MarketIndexSnapshot;
	lines: KLine[];
	meta: SourceMeta;
};

export type MarketIndustryMomentum = {
	code: string;
	name: string;
	change_percent: number;
	five_day_change_percent: number;
	twenty_day_change_percent: number;
	turnover_rate: number;
	rising_count: number;
	falling_count: number;
	main_net_inflow: number;
	leader_name?: string;
	leader_change_percent: number;
	score: number;
	meta: SourceMeta;
};

export type MarketFundFlow = {
	dimension: 'industry' | 'theme' | 'stock' | string;
	code: string;
	symbol?: string;
	name: string;
	price: number;
	change_percent: number;
	inflow: number;
	outflow: number;
	net_inflow: number;
	net_inflow_ratio: number;
	main_inflow: number;
	main_outflow: number;
	main_net_inflow: number;
	main_net_inflow_ratio: number;
	retail_inflow: number;
	retail_outflow: number;
	retail_net_inflow: number;
	retail_net_inflow_ratio: number;
	super_large_net_inflow: number;
	super_large_net_inflow_ratio: number;
	large_net_inflow: number;
	large_net_inflow_ratio: number;
	medium_net_inflow: number;
	medium_net_inflow_ratio: number;
	small_net_inflow: number;
	small_net_inflow_ratio: number;
	leader_symbol?: string;
	leader_name?: string;
	leader_price: number;
	leader_change_percent: number;
	leader_net_inflow_ratio: number;
	meta: SourceMeta;
};

export type MarketMarginPoint = {
	trade_date: string;
	financing_balance: number;
	securities_lending_balance: number;
	margin_balance: number;
	margin_balance_change: number;
	financing_buy_amount: number;
	financing_repay_amount: number;
	financing_net_buy_amount: number;
	securities_lending_sell_volume: number;
	securities_lending_repay_volume: number;
	meta: SourceMeta;
};

export type MarketBillboardItem = {
	trade_date: string;
	symbol: string;
	name: string;
	close_price: number;
	change_percent: number;
	turnover_rate: number;
	reason: string;
	summary?: string;
	buy_amount: number;
	sell_amount: number;
	net_amount: number;
	institution_buyers: number;
	buy_seats: number;
	sell_seats: number;
	meta: SourceMeta;
};

export type MarketBillboardSeat = {
	direction: 'buy' | 'sell' | string;
	rank: number;
	name: string;
	buy_amount: number;
	buy_ratio: number;
	sell_amount: number;
	sell_ratio: number;
	net_amount: number;
	institution: boolean;
	source_label?: string;
	source?: string;
	label_confidence?: string;
	label_note?: string;
};

export type MarketBillboardDetail = {
	trade_date: string;
	symbol: string;
	reason: string;
	buy_seats: MarketBillboardSeat[];
	sell_seats: MarketBillboardSeat[];
	meta: SourceMeta;
};

export type MarketResearchItem = {
	kind: 'announcement' | 'stock' | 'industry' | string;
	id: string;
	symbol?: string;
	stock_name?: string;
	industry_code?: string;
	industry_name?: string;
	title: string;
	content?: string;
	organization?: string;
	researchers?: string;
	rating?: string;
	previous_rating?: string;
	rating_change?: string;
	target_low?: number;
	target_high?: number;
	eps?: number;
	pe?: number;
	category?: string;
	published_at: string;
	url: string;
	meta: SourceMeta;
};

export type StockDirectoryEntry = {
	symbol: string;
	code: string;
	name: string;
};

export type StockDirectoryData = {
	stocks: StockDirectoryEntry[];
	total: number;
	source: string;
	updated_at: string;
	expires_at: string;
	stale: boolean;
};

export type HotStockRankSource = 'ths' | 'eastmoney';

export type HotStockRankEntry = {
	symbol: string;
	code: string;
	name: string;
	source_count: number;
	consensus_score: number;
	ranks: Partial<Record<HotStockRankSource, number>>;
};

export type HotStockRankSourceStatus = {
	id: HotStockRankSource;
	name: string;
	available: boolean;
	count: number;
	error?: string;
	fetched_at?: string;
};

export type HotStockRankData = {
	stocks: HotStockRankEntry[];
	sources: HotStockRankSourceStatus[];
	total: number;
	updated_at: string;
	expires_at: string;
	stale: boolean;
};

export type StockAIProfile = {
	primary_type: 'new_listing' | 'emotion_leader' | 'trend_capacity' | 'trend_growth' | 'range_watch' | 'weak_risk' | string;
	type_label: string;
	price_phase: string;
	market_role: string;
	tags: string[];
	confidence: number;
};

export type StockAITrend = {
	score: number;
	strength: string;
	phase: string;
	setup: string;
	latest_close: number;
	ma20: number;
	ma60: number;
	ma120: number;
	return_20d: number;
	return_60d: number;
	return_120d: number;
	range_position_60d: number;
	drawdown_from_high_120d: number;
	volume_ratio_5d_20d: number;
	atr_14_percent: number;
	support: number;
	resistance: number;
	invalidation: string;
	reasons: string[];
	history_days?: number;
	listing_return?: number;
	listing_high?: number;
	listing_low?: number;
	listing_range_position?: number;
	listing_drawdown?: number;
	average_turnover?: number;
	average_amount?: number;
	observed_volatility?: number;
};

export type StockAIDimensionScore = {
	key: string;
	label: string;
	score: number;
	weight: number;
	status: string;
	detail: string;
};

export type StockAIScorecard = {
	overall: number;
	grade: string;
	direction: string;
	conviction: string;
	dimensions: StockAIDimensionScore[];
	positive_signals: string[];
	negative_signals: string[];
};

export type StockAITimeframe = {
	key: string;
	label: string;
	window: number;
	score: number;
	state: string;
	return_percent: number;
	moving_average: number;
	slope_percent: number;
	above_moving_average: boolean;
	support: number;
	resistance: number;
};

export type StockAIRelativeStrength = {
	available: boolean;
	benchmark_symbol?: string;
	benchmark_name?: string;
	stock_return_20d: number;
	stock_return_60d: number;
	benchmark_return_20d: number;
	benchmark_return_60d: number;
	excess_return_20d: number;
	excess_return_60d: number;
	score: number;
	state: string;
	detail: string;
};

export type StockAISignal = {
	key: string;
	label: string;
	tone: 'positive' | 'negative' | 'neutral' | string;
	strength: number;
	detail: string;
};

export type StockAIPriceLevel = {
	label: string;
	price: number;
	detail: string;
};

export type StockAINextDayScenario = {
	key: string;
	name: string;
	priority: string;
	trigger: string;
	confirmation: string;
	action: string;
	invalidation: string;
};

export type StockAINextDayPlan = {
	bias: string;
	score: number;
	expectation: string;
	expected_low: number;
	expected_high: number;
	levels: StockAIPriceLevel[];
	scenarios: StockAINextDayScenario[];
	pre_open_checks: string[];
	opening_checks: string[];
	intraday_checks: string[];
	close_checks: string[];
};

export type StockAIRiskControl = {
	level: string;
	score: number;
	entry_reference: number;
	stop_price: number;
	stop_percent: number;
	take_profit_first: number;
	take_profit_second: number;
	risk_reward: number;
	suggested_position_min_percent: number;
	suggested_position_max_percent: number;
	single_trade_risk_percent: number;
	position_formula: string;
	rules: string[];
};

export type StockAIShortTerm = {
	state: string;
	limit_up_count_20d: number;
	max_limit_streak_20d: number;
	exact_limit_up_data: boolean;
	latest_streak: number;
	latest_open_count: number;
	latest_turnover_rate: number;
	average_amount_20d: number;
	return_5d: number;
	return_10d: number;
	tradability: string;
	reasons: string[];
};

export type StockAITheme = {
	primary: string;
	business: string;
	hot_theme?: string;
	is_hot: boolean;
	concepts: string[];
	source: string;
	as_of?: string;
	evidence?: string[];
	route: 'short_term' | 'trend' | string;
	trend_score: number;
	trend_stage: string;
	active_days: number;
	max_streak: number;
	role: string;
	description: string;
	hot_score: number;
	confidence: string;
	business_theme?: string;
	confirmed_themes?: StockAIThemeTag[];
	speculative_themes?: StockAIThemeTag[];
	evidence_items?: StockAIThemeEvidence[];
	resonance: StockAIThemeResonance;
};

export type StockAIThemeTag = {
	name: string;
	layer: string;
	confidence: string;
	score: number;
	evidence_count: number;
	detail: string;
};

export type StockAIThemeEvidence = {
	theme: string;
	type: string;
	source: string;
	title: string;
	url?: string;
	published_at?: string;
	snippet?: string;
	strength: number;
	freshness: number;
};

export type StockAIThemeResonance = {
	available: boolean;
	score: number;
	state: string;
	detail: string;
	stock_momentum: number;
	relative_strength: number;
	breadth: number;
	limit_up_energy: number;
	persistence: number;
	leader_position: number;
	evidence_quality: number;
	capital_diffusion: number;
};

export type StockAIFundamental = {
	available: boolean;
	score: number;
	quality: string;
	report_date: string;
	report_name: string;
	revenue: number;
	revenue_yoy: number;
	net_profit: number;
	net_profit_yoy: number;
	eps: number;
	roe: number;
	gross_margin: number;
	debt_ratio: number;
	operating_cash_flow_per_share: number;
	summary: string;
	source: string;
};

export type StockAIResearch = {
	available: boolean;
	score: number;
	coverage: string;
	report_count: number;
	organization_count: number;
	latest_rating?: string;
	rating_changes?: string[];
	summary: string;
	reports: MarketResearchItem[];
};

export type StockAINewsAnalysis = {
	available: boolean;
	window_days: number;
	article_count: number;
	source_count: number;
	latest_at?: string;
	tone: '偏多' | '中性' | '偏空' | '信息不足' | string;
	summary: string;
	catalysts: string[];
	risks: string[];
	keywords: string[];
	articles: NewsItem[];
	analysis_source: 'hermes-ai' | 'local-rules' | string;
};

export type StockAIMarket = {
	trade_date: string;
	phase: string;
	score: number;
	confidence: string;
	source: string;
};

export type StockAIActionPriceZone = {
	label: string;
	price_low: number;
	price_high: number;
	price_text: string;
	reason: string;
	action: string;
};

export type StockAIShortTermDecisionStage = {
	label: string;
	status: string;
	summary: string;
	required: string[];
	avoid: string[];
};

export type StockAIShortTermDecisionScenario = {
	name: string;
	tone: 'positive' | 'neutral' | 'negative' | string;
	condition: string;
	action: string;
};

export type StockAIShortTermQuantitativePlan = {
	baseline_date: string;
	stock: {
		reference_close: number;
		limit_up_percent: number;
		auction_change_min: number;
		auction_change_max: number;
		auction_price_min: number;
		auction_price_max: number;
		auction_amount_min: number;
		auction_amount_max: number;
		opening_drawdown_max: number;
		opening_amount_min: number;
		relative_index_min: number;
	};
	benchmark: {
		symbol: string;
		name: string;
		reference_close: number;
		auction_change_min: number;
		opening_change_min: number;
		failure_change: number;
	};
	theme: {
		name: string;
		limit_up_count: number;
		board_count: number;
		max_streak: number;
		active_days: number;
		minimum_positive_peers: number;
		maximum_weak_peers: number;
		positive_threshold: number;
		weak_threshold: number;
		source: string;
	};
	peers: Array<{
		symbol: string;
		name: string;
		role: string;
		streak: number;
		change_percent: number;
		has_quote: boolean;
	}>;
	missing?: string[];
};

export type StockAIShortTermPlaybook = {
	positioning: string;
	sentiment_cycle: string;
	expected_pattern: string;
	overnight_conclusion: string;
	data_status: string;
	quantitative?: StockAIShortTermQuantitativePlan;
	auction: StockAIShortTermDecisionStage;
	opening: StockAIShortTermDecisionStage;
	participation_conditions: string[];
	hold_conditions: string[];
	exit_conditions: string[];
	veto_conditions: string[];
	scenarios: StockAIShortTermDecisionScenario[];
};

export type StockAIActionPlan = {
	decision_mode?: 'short_term' | 'non_short' | string;
	decision_label?: string;
	decision_confidence?: number;
	horizon?: string;
	rationale?: string;
	pricing_source?: 'hermes-ai' | 'local-rules' | 'not-applicable' | string;
	current_action: string;
	entry?: StockAIActionPriceZone;
	hold?: StockAIActionPriceZone;
	take_profit?: StockAIActionPriceZone;
	stop_loss?: StockAIActionPriceZone;
	short_term_playbook?: StockAIShortTermPlaybook;
	/** 兼容价格决策升级前的本地分析缓存。 */
	forbidden?: StockAIActionPriceZone;
	entry_conditions: string[];
	hold_conditions: string[];
	avoid_conditions: string[];
	invalidation: string;
	position_hint: string;
};

export type StockAIEvidence = {
	category: string;
	title: string;
	detail: string;
	source: string;
	as_of?: string;
};

export type StockAIDataQuality = {
	key: string;
	status: 'ready' | 'limited' | 'missing' | string;
	message: string;
};

export type StockAITrendPoint = {
	date: string;
	close: number;
	ma20?: number;
	ma60?: number;
	ma120?: number;
};

export type StockAIAnalysis = {
	symbol: string;
	name: string;
	generated_at: string;
	quote: Quote;
	profile: StockAIProfile;
	conclusion: {
		headline: string;
		summary: string;
		action: string;
		best_path: string;
		main_risk: string;
		source: string;
	};
	trend: StockAITrend;
	short_term: StockAIShortTerm;
	theme: StockAITheme;
	fundamental?: StockAIFundamental;
	research?: StockAIResearch;
	stock_news?: StockAINewsAnalysis;
	theme_news?: StockAINewsAnalysis;
	market?: StockAIMarket;
	scorecard: StockAIScorecard;
	timeframes: StockAITimeframe[];
	relative_strength: StockAIRelativeStrength;
	signals: StockAISignal[];
	next_day: StockAINextDayPlan;
	risk_control: StockAIRiskControl;
	action_plan: StockAIActionPlan;
	risks: string[];
	evidence: StockAIEvidence[];
	data_quality: StockAIDataQuality[];
	chart: StockAITrendPoint[];
	ai: {
		status: 'ready' | 'rules' | 'unavailable' | 'error' | string;
		message?: string;
		model?: string;
	};
};

export type NewsItem = {
	id?: string;
	title: string;
	content?: string;
  url?: string;
  published_at?: string;
  tags?: string[];
	meta: SourceMeta;
};

export type BoardStock = {
	symbol: string;
	name: string;
	price: number;
	change: number;
	change_percent: number;
	volume: number;
	amount: number;
	total_market_cap: number;
	float_market_cap: number;
	main_net_inflow: number;
	limit_up_streak?: number;
	limit_up_days?: number;
	limit_up_count?: number;
	first_limit_time?: string;
	last_limit_time?: string;
	first_limit_date?: string;
	last_limit_date?: string;
	limit_regime?: '10cm' | '20cm' | '30cm' | string;
	rank_score?: number;
	rank_role?: string;
	meta: SourceMeta;
};

export type SectorMapNode = {
	id: string;
	name: string;
	description?: string;
	board_code?: string;
	board_name?: string;
	board_source?: string;
	change_percent: number;
	main_net_inflow: number;
	stocks: BoardStock[];
	stock_source?: string;
	match_status: 'matched' | 'unmatched' | string;
	matched_by?: string[];
	warnings?: string[];
	candidate_count?: number;
};

export type SectorMapGroup = {
	id: string;
	name: string;
	nodes: SectorMapNode[];
};

export type SectorMapTab = {
	id: string;
	name: string;
};

export type ThemeOverview = {
	theme: string;
	name: string;
	change_percent: number;
	main_net_inflow: number;
	rising_nodes: number;
	falling_nodes: number;
	matched_nodes: number;
	total_nodes: number;
	top_node?: string;
	top_node_change_percent: number;
	trend_score?: number;
	daily_strength_score?: number;
	five_day_strength_score?: number;
	trend_stage?: '主升' | '扩散' | '发酵' | '分歧' | '退潮' | string;
	limit_up_count?: number;
	board_count?: number;
	previous_count?: number;
	active_days?: number;
	max_streak?: number;
	leaders?: string[];
	source?: string;
	source_rank?: number;
	provider_rank?: number;
	source_strength?: number;
	trade_date?: string;
	snapshot_id?: string;
	carry_forward?: boolean;
	provisional?: boolean;
};

export type SectorMap = {
	theme: string;
	name: string;
	tabs: string[];
	theme_tabs?: SectorMapTab[];
	groups: SectorMapGroup[];
	meta: SourceMeta;
};

export type ThemeScreenSort = 'rank_score' | 'change_percent' | 'amount' | 'limit_up_streak';
export type ThemeScreenLane = 'all' | '10cm' | '20cm' | '30cm';

export type ThemeScreenPagination = {
	page: number;
	page_size: number;
	total: number;
	total_pages: number;
	has_more: boolean;
};

export type ThemeScreenData = {
	map: SectorMap;
	pagination: ThemeScreenPagination;
	snapshot_id: string;
	sort: ThemeScreenSort;
};

export type LimitUpLadderStock = {
	symbol: string;
	name: string;
	price: number;
	change_percent: number;
	current_change_percent?: number;
	amount: number;
	float_market_cap: number;
	turnover_rate: number;
	streak: number;
	first_limit_time?: string;
	last_limit_time?: string;
	open_count: number;
	industry?: string;
	days: number;
	count: number;
	streak_label?: string;
	board_type?: string;
	is_st: boolean;
	limit_regime: '10cm' | '20cm' | '30cm' | string;
	raw_concepts: string[];
	primary_theme: string;
	secondary_themes: string[];
	theme_confidence: number;
	theme_evidence: string[];
	theme_leader_role?: string;
	theme_source?: string;
	source?: string;
};

export type LimitUpLadderLevel = {
	level: number;
	label: string;
	count: number;
	stocks: LimitUpLadderStock[];
};

export type LimitUpLadderDay = {
	trade_date: string;
	limit_up_count: number;
	board_count: number;
	first_board_count: number;
	max_streak: number;
	reopened_count: number;
	st_count: number;
	total_amount: number;
	levels: LimitUpLadderLevel[];
};

export type LimitUpAdvanceStep = {
	from_level: number;
	to_level: number;
	base: number;
	success: number;
	rate: number;
};

export type LimitUpIndustryHeat = {
	name: string;
	count: number;
	board_count: number;
	max_streak: number;
	heat: number;
};

export type LimitUpConceptHeat = {
	name: string;
	count: number;
	board_count: number;
	max_streak: number;
	previous_count: number;
	heat: number;
	leaders: string[];
};

export type LimitUpLadderData = {
	session_status: string;
	current: LimitUpLadderDay;
	previous: LimitUpLadderDay;
	advance: LimitUpAdvanceStep[];
	industry_heat: LimitUpIndustryHeat[];
	concept_heat?: LimitUpConceptHeat[];
	concept_status?: 'ready' | 'degraded' | 'unavailable' | string;
	concept_error?: string;
	concept_meta?: SourceMeta;
	meta: SourceMeta;
};

export type MarketEmotionRaw = {
	limit_up_count: number;
	limit_down_count: number;
	broken_count: number;
	first_board_count: number;
	board_count: number;
	max_streak: number;
	reopened_count: number;
	final_break_rate: number;
	reopen_success_rate: number;
	previous_limit_up_return: number;
	previous_board_return: number;
	open_premium: number;
	core_return: number;
	advance_rate: number;
	theme_focus: number;
	leader_gap: number;
	ladder_continuity: number;
	high_sample_count: number;
	high_weak_count: number;
	high_kill: number;
	high_limit_down: number;
	high_average_return: number;
	high_down_rate: number;
	high_advance_rate: number;
	height_collapse: number;
	high_risk_score: number;
	mid_kill: number;
	low_kill: number;
	quote_coverage: number;
	limit_up_10cm: number;
	limit_up_20cm: number;
	limit_up_30cm: number;
	max_streak_10cm: number;
	max_streak_20cm: number;
	max_streak_30cm: number;
};

export type MarketEmotionPoint = {
	model_version: number;
	trade_date: string;
	emotion_score: number;
	phase: '冰点' | '启动/修复' | '发酵/主升' | '高潮' | '强分歧' | '退潮' | '混沌/过渡' | string;
	confidence: string;
	history_samples: number;
	raw: MarketEmotionRaw;
	scores: {
		heat: number;
		profit: number;
		structure: number;
		total: number;
	};
	source: string;
	updated_at: string;
};

export type MarketEmotionIntraday = {
	trade_date: string;
	base_trade_date?: string;
	session_status: string;
	status: string;
	breadth: string;
	summary: string;
	risk_score: number;
	confidence: string;
	metrics: {
		previous_max_streak: number;
		current_max_streak: number;
		height_collapse: number;
		high_levels: number[];
		high_sample_count: number;
		high_quote_count: number;
		high_average_return: number;
		high_down_count: number;
		high_down_rate: number;
		high_weak_count: number;
		high_weak_rate: number;
		high_severe_count: number;
		high_severe_rate: number;
		high_limit_down: number;
		high_advance_base: number;
		high_advance_count: number;
		high_advance_rate: number;
		limit_up_count: number;
		board_count: number;
		first_board_count: number;
	};
	updated_at: string;
	next_refresh_at: string;
	cache_ttl_seconds: number;
	stale: boolean;
};

export type MarketEmotionHistory = {
	points: MarketEmotionPoint[];
	latest?: MarketEmotionPoint;
	intraday?: MarketEmotionIntraday;
	intraday_error?: string;
	cache: {
		mode: string;
		cached_days: number;
		bootstrap_days: number;
		last_external_sync?: string;
		last_error?: string;
		updated_at?: string;
	};
};

export type MasteryTraderSummary = {
	id: string;
	name: string;
	document_count: number;
	character_count: number;
	reading_minutes: number;
	placeholder_count: number;
	tags?: string[];
	quote?: string;
	source_url: string;
};

export type MasterySnapshot = {
	traders: MasteryTraderSummary[];
	fetched_at: string;
	source_url: string;
	source_commit?: string;
	stale: boolean;
	knowledge_status: 'ready' | 'limited' | string;
	knowledge_message?: string;
};

export type MasteryDocument = {
	id: string;
	title: string;
	kind: 'deep_report' | 'study_notes' | 'article' | string;
	content: string;
	source_url: string;
	character_count: number;
	placeholder_count: number;
	tags?: string[];
};

export type MasteryTraderDetail = MasteryTraderSummary & {
	documents: MasteryDocument[];
};

export type StreamMessage = {
	type: 'quotes' | 'error';
	quotes?: Quote[];
  error?: string;
  fetched_at: string;
};

export type ReviewSource = {
	id: 'wechat' | 'xueqiu' | 'taoguba' | string;
	name: string;
	status: string;
	message: string;
	import_ready: boolean;
	sync_ready: boolean;
};

export type ReviewAuthor = {
	id: string;
	source: string;
	name: string;
	post_count: number;
	latest_at: string;
};

export type ReviewAuthorDeleteResult = {
	author_id: string;
	author_name: string;
	source: string;
	posts_deleted: number;
	subscriptions_deleted: number;
	summary_cache_cleared: boolean;
};

export type ReviewPost = {
	id: string;
	source: string;
	external_id: string;
	author_id: string;
	author_name: string;
	title: string;
	digest: string;
	content_text: string;
	cover_url?: string;
	original_url: string;
	published_at: string;
	fetched_at: string;
	related_stocks: string[];
	related_themes: string[];
	read: boolean;
	favorite: boolean;
	ai_summary?: string;
	ai_key_points: string[];
	ai_outlook?: string;
	ai_analyzed_at?: string;
	ai_error?: string;
};

export type ReviewSubscription = {
	id: string;
	source: string;
	name: string;
	homepage_url: string;
	external_id?: string;
	config_id?: string;
	enabled: boolean;
	last_sync_at?: string;
	next_sync_at?: string;
	last_status: string;
	last_error?: string;
	created_at: string;
};

export type ReviewSyncResult = {
	subscription_id: string;
	found: number;
	imported: number;
	analyzed: number;
	error?: string;
};

export type ReviewDailyConsensus = {
	topic: string;
	conclusion: string;
	support_count: number;
	authors: string[];
	evidence: string[];
};

export type ReviewDailyDisagreement = {
	topic: string;
	views: string[];
	authors: string[];
	positions: Array<{
		author: string;
		stance: string;
		view: string;
		evidence: string;
	}>;
};

export type ReviewDailyStockView = {
	name: string;
	symbol?: string;
	logic: string;
	support_count: number;
	authors: string[];
	evidence: string[];
	trigger?: string;
	invalidation?: string;
	risk?: string;
};

export type ReviewDailySummarySource = {
	post_id?: string;
	author: string;
	title: string;
	source: string;
	url?: string;
	published_at?: string;
};

export type ReviewDailyAuthorView = {
	author: string;
	source: string;
	article_count: number;
	available_article_count: number;
	time_range: string;
	core_view: string;
	market_interpretation: string;
	view_evolution: string[];
	themes: string[];
	today_surprises: ReviewDailyStockView[];
	tomorrow_focus: ReviewDailyStockView[];
	tomorrow_outlook: string;
	catalysts: string[];
	risks: string[];
	confidence: string;
	evidence: string[];
	sources: ReviewDailySummarySource[];
};

export type ReviewDailySummary = {
	trade_date: string;
	generated_at: string;
	prompt_version: string;
	window_start: string;
	window_end: string;
	freshness_rule: string;
	article_count: number;
	author_count: number;
	authors: string[];
	sources: ReviewDailySummarySource[];
	author_views: ReviewDailyAuthorView[];
	executive_summary: string;
	market_regime: string;
	market_analysis: string;
	market_framework: {
		cycle: string;
		capital_pricing: string;
		direction_competition: string;
		trading_method: string;
	};
	consensus: ReviewDailyConsensus[];
	disagreements: ReviewDailyDisagreement[];
	scenarios: Array<{
		key: 'base' | 'strong' | 'weak';
		name: string;
		summary: string;
		trigger: string;
		confirmation: string;
		invalidation: string;
		focus: string[];
	}>;
	directions: Array<{
		name: string;
		stance: string;
		summary: string;
		supporting_authors: string[];
		opposing_authors: string[];
		stocks: string[];
		trigger: string;
		invalidation: string;
		risks: string[];
	}>;
	today_surprises: ReviewDailyStockView[];
	tomorrow_focus: ReviewDailyStockView[];
	tomorrow_outlook: string;
	tomorrow_playbook: {
		pre_open: string[];
		opening: string[];
		intraday: string[];
		close: string[];
	};
	catalysts: string[];
	risks: string[];
	verification_checklist: string[];
	limitations: string[];
};

export type ReviewDailySummaryJob = {
	trade_date: string;
	window_start?: string;
	window_end?: string;
	freshness_rule?: string;
	status: 'idle' | 'running' | 'succeeded' | 'failed';
	stage: 'idle' | 'preparing' | 'authors' | 'finalizing' | 'completed' | 'failed' | 'interrupted';
	completed_authors: number;
	total_authors: number;
	article_count: number;
	message: string;
	error?: string;
	started_at?: string;
	updated_at?: string;
	completed_at?: string;
	summary_available: boolean;
};

export type ReviewDailySummaryWindow = {
	trade_date: string;
	window_start: string;
	window_end: string;
	freshness_rule: string;
};

export type ReviewDailyValidationSnapshot = {
	captured_at: string;
	trade_date: string;
	indexes: Array<{ id: string; name: string; change_percent: number; trade_date?: string; source?: string }>;
	emotion?: {
		phase: string;
		emotion_score: number;
		heat: number;
		profit: number;
		structure: number;
		limit_up_count: number;
		limit_down_count: number;
		broken_count: number;
		first_board_count: number;
		board_count: number;
		max_streak: number;
		previous_limit_up_return: number;
		final_break_rate: number;
		advance_rate: number;
		high_average_return: number;
		high_advance_rate: number;
		height_collapse: number;
		high_risk_score: number;
		quote_coverage: number;
		confidence?: string;
	};
	intraday?: {
		trade_date?: string;
		status: string;
		breadth: string;
		risk_score: number;
		current_max_streak: number;
		height_collapse: number;
		high_average_return: number;
		high_down_rate: number;
		high_advance_rate: number;
		limit_up_count: number;
		board_count: number;
		first_board_count: number;
		confidence?: string;
	};
	themes: Array<{ name: string; change_percent: number; net_inflow: number; rising_count: number; falling_count: number; limit_up_count: number; max_streak: number; trend_score: number; stage?: string; leaders?: string[]; source?: string }>;
	industries: Array<{ name: string; change_percent: number; net_inflow: number; rising_count: number; falling_count: number; score: number; leader_name?: string }>;
	flows: Array<{ dimension: string; name: string; change_percent: number; net_inflow: number; main_net_inflow: number; leader_name?: string }>;
	limit_up: { current_trade_date: string; previous_trade_date: string; current_count: number; previous_count: number; current_board_count: number; previous_board_count: number; current_max_streak: number; previous_max_streak: number; concepts?: Array<{ name: string; limit_up_count: number; max_streak: number; trend_score: number; leaders?: string[] }> };
	stocks: Array<{ name: string; symbol: string; open: number; high: number; low: number; price: number; previous_close: number; change_percent: number; trade_date?: string; limit_up_streak?: number; source?: string; matched: boolean; match_note?: string }>;
	data_quality: string[];
};

export type ReviewDailyValidation = {
	summary_date: string;
	verification_date: string;
	generated_at: string;
	prompt_version: string;
	summary_hash: string;
	status: string;
	ai_status: string;
	ai_message?: string;
	score: number;
	coverage: number;
	correct_count: number;
	partial_count: number;
	wrong_count: number;
	unverified_count: number;
	headline: string;
	actual_scenario: string;
	market: { expected_regime: string; actual_phase: string; verdict: string; summary: string; evidence: string[] };
	scenario: { expected_key: string; expected_name: string; actual_key: string; actual_name: string; verdict: string; summary: string };
	directions: Array<{ name: string; verdict: string; score: number; rank?: number; actual_change?: number; evidence: string[] }>;
	stocks: Array<{ name: string; symbol?: string; verdict: string; actual_change?: number; open_change?: number; intraday_high?: number; intraday_low?: number; trigger_hit?: string; invalidation_hit?: string; summary: string; evidence: string[] }>;
	checklist: Array<{ text: string; verdict: string; evidence: string[] }>;
	realized_risks: string[];
	lessons: string[];
	data_quality: string[];
	snapshot: ReviewDailyValidationSnapshot;
};

export type ReviewDailyValidationJob = {
	summary_date: string;
	verification_date?: string;
	status: 'idle' | 'running' | 'succeeded' | 'failed' | string;
	stage: 'idle' | 'collecting' | 'evaluating' | 'explaining' | 'completed' | 'failed' | string;
	message: string;
	error?: string;
	started_at?: string;
	updated_at?: string;
	completed_at?: string;
	result_available: boolean;
};

export async function resolveBackendConfig(input: ResolveInput = {}): Promise<BackendConfig> {
  const bridge = input.bridge ?? globalThis.window?.aStock;
  const bridged = await bridge?.getBackendConfig().catch(() => undefined);
  if (bridged?.backendUrl) {
    return normalizeConfig(bridged);
  }

  const env = input.env ?? import.meta.env;
  return normalizeConfig({
    backendUrl: env.VITE_A_STOCK_BACKEND_URL || 'http://127.0.0.1:20081',
    token: env.VITE_A_STOCK_TOKEN || '',
  });
}

export function buildStreamUrl(config: BackendConfig, symbols: string[], intervalMS: number): string {
  const url = new URL('/api/v1/ws/stream', config.backendUrl);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  url.searchParams.set('symbols', symbols.join(','));
  url.searchParams.set('interval_ms', String(intervalMS));
  if (config.token) {
    url.searchParams.set('token', config.token);
  }
  return url.toString();
}

export async function requestJSON<T>(config: BackendConfig, path: string, init: RequestInit = {}): Promise<T> {
	const headers = new Headers(init.headers);
	if (config.token) headers.set('Authorization', `Bearer ${config.token}`);
	const response = await fetch(new URL(path, config.backendUrl), {
		...init,
		headers,
	});
  if (!response.ok) {
    const text = await response.text();
		let message = text;
		try {
			message = (JSON.parse(text) as { error?: string }).error || text;
		} catch {
			// Keep plain-text upstream errors readable.
		}
		throw new Error(message || `HTTP ${response.status}`);
  }
	if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

function normalizeConfig(config: BackendConfig): BackendConfig {
  return {
    backendUrl: config.backendUrl.replace(/\/+$/, ''),
    token: config.token || '',
  };
}
