package monitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"easy-stock/backend/internal/sqliteutil"
)

var memoryDBSeq atomic.Uint64

type Store struct {
	db *sql.DB
}

// 触发状态与事件必须使用同一事务，避免事件失败后丢失触发资格。
type monitorWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// OpenStore 打开监控库。路径为空时开内存库，供失败降级和测试使用。
func OpenStore(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = ":memory:"
	}
	dataSource := path
	if path == ":memory:" {
		// 内存库名必须带进程内唯一序号：Windows 时间片
		// 可能粗到毫秒，cache=shared 会让同名内存库事实上共享数据。
		dataSource = fmt.Sprintf("file:easy-stock-monitor-%d-%d?mode=memory&cache=shared",
			time.Now().UnixNano(), memoryDBSeq.Add(1))
	} else if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("创建监控数据目录失败: %w", err)
		}
	}
	db, err := sqliteutil.Open(dataSource)
	if err != nil {
		return nil, fmt.Errorf("打开监控数据库失败: %w", err)
	}
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

const monitorSchema = `
	CREATE TABLE IF NOT EXISTS monitor_rules (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		severity TEXT NOT NULL,
		scope TEXT NOT NULL,
		symbols TEXT NOT NULL DEFAULT '[]',
		filters TEXT NOT NULL DEFAULT '{}',
		conditions TEXT NOT NULL DEFAULT '[]',
		logic TEXT NOT NULL DEFAULT 'and',
		cooldown_seconds INTEGER NOT NULL DEFAULT 300,
		notify_channels TEXT NOT NULL DEFAULT '[]',
		source TEXT NOT NULL DEFAULT 'manual',
		lot_id TEXT NOT NULL DEFAULT '',
		remind_date TEXT NOT NULL DEFAULT '',
		lead_days INTEGER NOT NULL DEFAULT 0,
		trigger_count INTEGER NOT NULL DEFAULT 0,
		last_triggered_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_monitor_rules_enabled ON monitor_rules(enabled, type);
	CREATE INDEX IF NOT EXISTS idx_monitor_rules_source ON monitor_rules(source, lot_id);

	CREATE TABLE IF NOT EXISTS monitor_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		rule_id TEXT NOT NULL,
		rule_name TEXT NOT NULL,
		rule_type TEXT NOT NULL,
		symbol TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL DEFAULT '',
		severity TEXT NOT NULL,
		trigger_kind TEXT NOT NULL,
		message TEXT NOT NULL,
		detail TEXT NOT NULL DEFAULT '{}',
		price REAL NOT NULL DEFAULT 0,
		change_percent REAL NOT NULL DEFAULT 0,
		trade_date TEXT NOT NULL DEFAULT '',
		notify_status TEXT NOT NULL DEFAULT 'skipped',
		notify_error TEXT NOT NULL DEFAULT '',
		notified_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_monitor_events_created ON monitor_events(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_monitor_events_rule ON monitor_events(rule_id, symbol, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_monitor_events_date ON monitor_events(trade_date);

	CREATE TABLE IF NOT EXISTS monitor_lots (
		id TEXT PRIMARY KEY,
		symbol TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		quantity INTEGER NOT NULL,
		cost_price REAL NOT NULL,
		buy_date TEXT NOT NULL DEFAULT '',
		target_pct REAL NOT NULL DEFAULT 0,
		stop_pct REAL NOT NULL DEFAULT 0,
		remind_date TEXT NOT NULL DEFAULT '',
		lead_days INTEGER NOT NULL DEFAULT 0,
		source TEXT NOT NULL DEFAULT 'manual',
		position_account TEXT NOT NULL DEFAULT '',
		detached INTEGER NOT NULL DEFAULT 0,
		enabled INTEGER NOT NULL DEFAULT 1,
		note TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_monitor_lots_symbol ON monitor_lots(symbol);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_monitor_lots_position
		ON monitor_lots(position_account, symbol) WHERE source='position';

	CREATE TABLE IF NOT EXISTS monitor_state (
		state_key TEXT PRIMARY KEY,
		armed INTEGER NOT NULL DEFAULT 1,
		last_value REAL NOT NULL DEFAULT 0,
		last_fired_at TEXT NOT NULL DEFAULT '',
		last_fired_date TEXT NOT NULL DEFAULT '',
		cooldown_until TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS monitor_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task TEXT NOT NULL,
		started_at TEXT NOT NULL,
		finished_at TEXT NOT NULL,
		evaluated INTEGER NOT NULL DEFAULT 0,
		triggered INTEGER NOT NULL DEFAULT 0,
		error TEXT NOT NULL DEFAULT '',
		duration_ms INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_monitor_runs_started ON monitor_runs(started_at DESC);

	-- 作用域清单快照：巡检组合存在浏览器本地，后台引擎读不到，
	-- 只能由前端把「用户当前看到的持仓」推一份过来。
	CREATE TABLE IF NOT EXISTS monitor_scope_snapshot (
		scope TEXT PRIMARY KEY,
		payload TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	-- 监控中心自己的关注仓：引擎按 scope=watchlist 盯这里，与
	-- 其他模块的清单各自维护。
	CREATE TABLE IF NOT EXISTS monitor_watchlist (
		symbol TEXT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);
`

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, monitorSchema)
	if err != nil {
		return fmt.Errorf("监控库迁移失败: %w", err)
	}
	return nil
}

func utcNow() time.Time { return time.Now().UTC() }

func writeTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTimeText(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &parsed
}

// ---------------------------------------------------------------- 规则 CRUD

func (s *Store) UpsertRule(ctx context.Context, rule Rule) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldType, oldScope, oldSymbols, oldFilters, oldConditions, oldLogic, oldDate string
	var oldLeadDays int
	err = tx.QueryRowContext(ctx, `SELECT type,scope,symbols,filters,conditions,logic,remind_date,lead_days
		FROM monitor_rules WHERE id=?`, rule.ID).Scan(&oldType, &oldScope, &oldSymbols, &oldFilters, &oldConditions, &oldLogic, &oldDate, &oldLeadDays)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	reset := err == nil && (oldType != rule.Type || oldScope != rule.Scope || oldSymbols != marshalJSON(rule.Symbols) ||
		oldFilters != marshalJSON(rule.Filters) || oldConditions != marshalJSON(rule.Conditions) || oldLogic != rule.Logic ||
		oldDate != rule.RemindDate || oldLeadDays != rule.LeadDays)
	_, err = tx.ExecContext(ctx, `INSERT INTO monitor_rules
		(id,name,type,enabled,severity,scope,symbols,filters,conditions,logic,
		 cooldown_seconds,notify_channels,source,lot_id,remind_date,lead_days,
		 trigger_count,last_triggered_at,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  name=excluded.name, type=excluded.type, enabled=excluded.enabled,
		  severity=excluded.severity, scope=excluded.scope, symbols=excluded.symbols,
		  filters=excluded.filters, conditions=excluded.conditions, logic=excluded.logic,
		  cooldown_seconds=excluded.cooldown_seconds, notify_channels=excluded.notify_channels,
		  source=excluded.source, lot_id=excluded.lot_id, remind_date=excluded.remind_date,
		  lead_days=excluded.lead_days, updated_at=excluded.updated_at`,
		rule.ID, rule.Name, rule.Type, boolInt(rule.Enabled), rule.Severity, rule.Scope,
		marshalJSON(rule.Symbols), marshalJSON(rule.Filters), marshalJSON(rule.Conditions), rule.Logic,
		rule.CooldownSeconds, marshalJSON(rule.NotifyChannels), rule.Source, rule.LotID,
		rule.RemindDate, rule.LeadDays, rule.TriggerCount,
		writeTime(orTime(rule.LastTriggeredAt)), writeTime(rule.CreatedAt), writeTime(rule.UpdatedAt))
	if err != nil {
		return fmt.Errorf("保存监控规则失败: %w", err)
	}
	if reset {
		prefix := rule.ID + "|"
		if _, err := tx.ExecContext(ctx, `DELETE FROM monitor_state WHERE substr(state_key,1,length(?))=?`, prefix, prefix); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func orTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Store) ListRules(ctx context.Context, ruleType string, enabledOnly bool) ([]Rule, error) {
	query := `SELECT id,name,type,enabled,severity,scope,symbols,filters,conditions,logic,
		cooldown_seconds,notify_channels,source,lot_id,remind_date,lead_days,
		trigger_count,last_triggered_at,created_at,updated_at FROM monitor_rules WHERE 1=1`
	args := []any{}
	if ruleType != "" {
		query += ` AND type=?`
		args = append(args, ruleType)
	}
	if enabledOnly {
		query += ` AND enabled=1`
	}
	query += ` ORDER BY type, id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := []Rule{}
	for rows.Next() {
		var rule Rule
		var symbols, filters, conditions, channels string
		var lastTriggered, created, updated string
		var enabled int
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Type, &enabled, &rule.Severity, &rule.Scope,
			&symbols, &filters, &conditions, &rule.Logic, &rule.CooldownSeconds, &channels,
			&rule.Source, &rule.LotID, &rule.RemindDate, &rule.LeadDays, &rule.TriggerCount,
			&lastTriggered, &created, &updated); err != nil {
			return nil, err
		}
		rule.Enabled = enabled == 1
		unmarshalJSON(symbols, &rule.Symbols)
		unmarshalJSON(filters, &rule.Filters)
		unmarshalJSON(conditions, &rule.Conditions)
		unmarshalJSON(channels, &rule.NotifyChannels)
		rule.LastTriggeredAt = parseTimeText(lastTriggered)
		rule.CreatedAt = parseTime(created)
		rule.UpdatedAt = parseTime(updated)
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func parseTime(value string) time.Time {
	if parsed := parseTimeText(value); parsed != nil {
		return *parsed
	}
	return time.Time{}
}

func (s *Store) DeleteRule(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM monitor_rules WHERE id=?`, id)
	if err == nil {
		// 托管规则跟随批次删除时，残留状态行只会越攒越多，顺手清掉。
		_, err = s.db.ExecContext(ctx, `DELETE FROM monitor_state WHERE state_key LIKE ?`, id+"|%")
	}
	return err
}

func (s *Store) SetRuleEnabled(ctx context.Context, id string, enabled bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE monitor_rules SET enabled=? WHERE id=?`, boolInt(enabled), id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return errors.New("规则不存在")
	}
	return nil
}

// SetLotEnabled 同步启停批次本体：批次托管规则的开关跟随批次，
// 单独开一条没有批次的托管规则会让人误以为批次还在被监控。
func (s *Store) SetLotEnabled(ctx context.Context, id string, enabled bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE monitor_lots SET enabled=? WHERE id=?`, boolInt(enabled), id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return errors.New("批次不存在")
	}
	return nil
}

func (s *Store) CountRulesBySource(ctx context.Context, source string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM monitor_rules WHERE source=?`, source).Scan(&count)
	return count, err
}

// ---------------------------------------------------------------- 事件

func (s *Store) AppendEvent(ctx context.Context, event Event) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := appendMonitorEvent(ctx, tx, &event)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func appendMonitorEvent(ctx context.Context, writer monitorWriter, event *Event) (int64, error) {
	if event.NotifyStatus == "" {
		event.NotifyStatus = NotifySkipped
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = utcNow()
	}
	result, err := writer.ExecContext(ctx, `INSERT INTO monitor_events
		(rule_id,rule_name,rule_type,symbol,name,severity,trigger_kind,message,detail,
		 price,change_percent,trade_date,notify_status,notify_error,notified_at,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		event.RuleID, event.RuleName, event.RuleType, event.Symbol, event.Name, event.Severity,
		event.TriggerKind, event.Message, marshalJSON(event.Detail), event.Price,
		event.ChangePercent, event.TradeDate, event.NotifyStatus, event.NotifyError,
		writeTime(orTime(event.NotifiedAt)), writeTime(event.CreatedAt))
	if err != nil {
		return 0, fmt.Errorf("写入监控事件失败: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	_, err = writer.ExecContext(ctx, `UPDATE monitor_rules
		SET trigger_count=trigger_count+1, last_triggered_at=?
		WHERE id=?`, writeTime(event.CreatedAt), event.RuleID)
	if err != nil {
		return 0, err
	}
	event.ID = id
	return id, nil
}

func (s *Store) UpdateEventNotify(ctx context.Context, id int64, status, message string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE monitor_events
		SET notify_status=?, notify_error=?, notified_at=? WHERE id=?`,
		status, strings.TrimSpace(message), writeTime(at), id)
	return err
}

func eventWhere(filter EventFilter) (string, []any) {
	query := ` WHERE 1=1`
	args := []any{}
	if filter.RuleID != "" {
		query += ` AND rule_id=?`
		args = append(args, filter.RuleID)
	}
	if filter.RuleType != "" {
		query += ` AND rule_type=?`
		args = append(args, filter.RuleType)
	}
	if filter.Symbol != "" {
		query += ` AND symbol=?`
		args = append(args, filter.Symbol)
	}
	if filter.Severity != "" {
		query += ` AND severity=?`
		args = append(args, filter.Severity)
	}
	if filter.TradeDate != "" {
		query += ` AND trade_date=?`
		args = append(args, filter.TradeDate)
	}
	if filter.SinceID > 0 {
		query += ` AND id>?`
		args = append(args, filter.SinceID)
	}
	return query, args
}

func (s *Store) CountEvents(ctx context.Context, filter EventFilter) (int, error) {
	where, args := eventWhere(filter)
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM monitor_events`+where, args...).Scan(&count)
	return count, err
}

func (s *Store) ListEvents(ctx context.Context, filter EventFilter) ([]Event, error) {
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}
	query := `SELECT id,rule_id,rule_name,rule_type,symbol,name,severity,trigger_kind,message,
		detail,price,change_percent,trade_date,notify_status,notify_error,notified_at,created_at
		FROM monitor_events`
	where, args := eventWhere(filter)
	query += where
	if filter.OldestFirst {
		query += ` ORDER BY id ASC LIMIT ?`
	} else {
		query += ` ORDER BY id DESC LIMIT ?`
	}
	args = append(args, filter.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var event Event
		var detail, notified, created string
		if err := rows.Scan(&event.ID, &event.RuleID, &event.RuleName, &event.RuleType, &event.Symbol,
			&event.Name, &event.Severity, &event.TriggerKind, &event.Message, &detail, &event.Price,
			&event.ChangePercent, &event.TradeDate, &event.NotifyStatus, &event.NotifyError,
			&notified, &created); err != nil {
			return nil, err
		}
		unmarshalJSON(detail, &event.Detail)
		if parseTimeText(notified) != nil {
			t := *parseTimeText(notified)
			event.NotifiedAt = &t
		}
		event.CreatedAt = parseTime(created)
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) CountEventsToday(ctx context.Context, ruleID, tradeDate string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM monitor_events
		WHERE rule_id=? AND trade_date=?`, ruleID, tradeDate).Scan(&count)
	return count, err
}

func (s *Store) EventSummary(ctx context.Context, tradeDate string) (total, critical int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN severity=? THEN 1 ELSE 0 END),0)
		FROM monitor_events WHERE trade_date=?`, SeverityCritical, tradeDate).Scan(&total, &critical)
	return
}

func (s *Store) DeviationCounts(ctx context.Context, start, end string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT symbol, COUNT(*) FROM monitor_events
		WHERE rule_type=? AND trade_date>=? AND trade_date<=? AND symbol<>'' GROUP BY symbol`, RuleTypeDeviation, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var symbol string
		var count int
		if err := rows.Scan(&symbol, &count); err != nil {
			return nil, err
		}
		counts[symbol] = count
	}
	return counts, rows.Err()
}

func (s *Store) CountUnnotifiedToday(ctx context.Context, tradeDate string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM monitor_events
		WHERE trade_date=? AND notify_status='queued'`, tradeDate).Scan(&count)
	return count, err
}

func (s *Store) DeleteEvent(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM monitor_events WHERE id=?`, id)
	return err
}

func (s *Store) ClearEvents(ctx context.Context, before int64) error {
	if before > 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM monitor_events WHERE id<?`, before)
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM monitor_events`)
	return err
}

// ---------------------------------------------------------------- 触发门

// allowTrigger 是冷却、穿越判定与每日上限三件事的合体。
//
// 返回 true 表示"这次应该发一条事件"；false 时不写事件但会更新观测值，
// 让下次穿越判定有上一值可用。
func (s *Store) allowTrigger(ctx context.Context, rule Rule, symbol string, condition Condition, value float64, now time.Time, tradeDate string) (bool, error) {
	return s.observeTrigger(ctx, rule, symbol, condition, value, now, tradeDate, true)
}

// observeTrigger 始终保存观测值，eligible=false 时只观测，不消耗触发资格。
func (s *Store) observeTrigger(ctx context.Context, rule Rule, symbol string, condition Condition, value float64, now time.Time, tradeDate string, eligible bool) (bool, error) {
	return s.observeTriggerWith(ctx, s.db, rule, symbol, condition, value, now, tradeDate, eligible)
}

func (s *Store) observeEvent(ctx context.Context, rule Rule, symbol string, condition Condition, value float64, now time.Time, tradeDate string, eligible bool, event *Event) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	allowed, err := s.observeTriggerWith(ctx, tx, rule, symbol, condition, value, now, tradeDate, eligible)
	if err != nil {
		return false, err
	}
	if allowed {
		if _, err := appendMonitorEvent(ctx, tx, event); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return allowed, nil
}

func (s *Store) observeTriggerWith(ctx context.Context, writer monitorWriter, rule Rule, symbol string, condition Condition, value float64, now time.Time, tradeDate string, eligible bool) (bool, error) {
	fieldKey := condition.Field
	for _, other := range rule.Conditions {
		if other.Field == condition.Field && marshalJSON(other) != marshalJSON(condition) {
			fieldKey += "|" + marshalJSON(condition)
			break
		}
	}
	key := rule.ID + "|" + symbol + "|" + fieldKey
	var armed int
	var lastValue float64
	var lastFiredAt, lastFiredDate, cooldownUntil string
	err := writer.QueryRowContext(ctx, `SELECT armed, last_value, last_fired_at, last_fired_date, cooldown_until
		FROM monitor_state WHERE state_key=?`, key).
		Scan(&armed, &lastValue, &lastFiredAt, &lastFiredDate, &cooldownUntil)
	havePrevious := err == nil
	switch {
	case errors.Is(err, sql.ErrNoRows):
		armed = 1
	case err != nil:
		return false, err
	}
	armedNow := armed == 1
	satisfied := conditionSatisfied(condition, value)
	fired := false
	switch {
	case isAbnormalField(condition.Field) || rule.Type == RuleTypeDate ||
		(rule.Type == RuleTypeSignal && isSignalField(condition.Field)):
		// 布尔、到期与技术信号：一天最多一次，冷却只影响当日内的重复。
		fired = satisfied && lastFiredDate != tradeDate
	case condition.Op == OpCrossUp:
		fired = satisfied && havePrevious && lastValue < condition.Value
	case condition.Op == OpCrossDown:
		fired = satisfied && havePrevious && lastValue > condition.Value
	default:
		if cooldownActive(cooldownUntil, now) {
			fired = false
		} else {
			// 阈值型状态机：进入区间触发并解除武装，回到区间外重新武装。
			fired = armedNow && satisfied
		}
	}
	fired = fired && eligible && !cooldownActive(cooldownUntil, now)
	if fired {
		// 每规则每日上限是最后一道闸；到达上限后规则当天沉默。
		var count int
		if err := writer.QueryRowContext(ctx, `SELECT COUNT(*) FROM monitor_events WHERE rule_id=? AND trade_date=?`, rule.ID, tradeDate).Scan(&count); err != nil {
			return false, err
		}
		if count >= dailyEventCap {
			fired = false
		}
	}
	if err := s.upsertState(ctx, writer, key, value, fired, now, tradeDate, satisfied, rule.CooldownSeconds, armedNow); err != nil {
		return false, err
	}
	return fired, nil
}

const dailyEventCap = 200

func hasPrev(lastValue float64) bool {
	return lastValue != 0
}

func cooldownActive(until string, now time.Time) bool {
	parsed := parseTimeText(until)
	if parsed == nil {
		return false
	}
	return parsed.After(now)
}

// upsertState 写回状态机。fired 时记冷却与武装解除；
// 未 fired 时只刷新观测值并按"当前值是否仍在区间内"决定武装，
// 这样一次新鲜部署（无历史状态）也能直接进入正确状态。
func (s *Store) upsertState(ctx context.Context, writer monitorWriter, key string, value float64, fired bool, now time.Time, tradeDate string, satisfied bool, cooldownSeconds int, wasArmed bool) error {
	armed := 0
	if wasArmed || !satisfied {
		armed = 1
	}
	lastFiredDate := ""
	cooldownUntil := ""
	if fired {
		armed = 0
		lastFiredDate = tradeDate
		if cooldownSeconds > 0 {
			cooldownUntil = writeTime(now.Add(time.Duration(cooldownSeconds) * time.Second))
		}
	}
	_, err := writer.ExecContext(ctx, `INSERT INTO monitor_state
		(state_key,armed,last_value,last_fired_at,last_fired_date,cooldown_until,updated_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(state_key) DO UPDATE SET
		  armed=excluded.armed,
		  last_value=excluded.last_value,
		  last_fired_date=CASE WHEN excluded.last_fired_date<>'' THEN excluded.last_fired_date ELSE last_fired_date END,
		  cooldown_until=CASE WHEN excluded.cooldown_until<>'' THEN excluded.cooldown_until ELSE cooldown_until END,
		  updated_at=excluded.updated_at`,
		key, armed, value, "", lastFiredDate, cooldownUntil, writeTime(now))
	return err
}

// ---------------------------------------------------------------- 批次

func (s *Store) archivePositionLot(ctx context.Context, lotID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE monitor_lots SET quantity=0,enabled=0,updated_at=?
		WHERE id=? AND source=? AND detached=0`, writeTime(utcNow()), lotID, SourcePosition)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count == 0 {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE monitor_rules SET enabled=0 WHERE lot_id=? AND source=?`, lotID, SourceLot); err != nil {
		return err
	}
	for _, id := range []string{lotPriceRuleID(lotID), lotDateRuleID(lotID)} {
		prefix := id + "|"
		if _, err := tx.ExecContext(ctx, `DELETE FROM monitor_state WHERE substr(state_key,1,length(?))=?`, prefix, prefix); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpsertLot(ctx context.Context, lot Lot) error {
	if lot.CreatedAt.IsZero() {
		lot.CreatedAt = utcNow()
	}
	lot.UpdatedAt = utcNow()
	_, err := s.db.ExecContext(ctx, `INSERT INTO monitor_lots
		(id,symbol,name,quantity,cost_price,buy_date,target_pct,stop_pct,remind_date,lead_days,
		 source,position_account,detached,enabled,note,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  symbol=excluded.symbol, name=excluded.name, quantity=excluded.quantity,
		  cost_price=excluded.cost_price, buy_date=excluded.buy_date,
		  target_pct=excluded.target_pct, stop_pct=excluded.stop_pct,
		  remind_date=excluded.remind_date, lead_days=excluded.lead_days,
		  detached=excluded.detached, enabled=excluded.enabled, note=excluded.note,
		  updated_at=excluded.updated_at`,
		lot.ID, lot.Symbol, lot.Name, lot.Quantity, lot.CostPrice, lot.BuyDate,
		lot.TargetPct, lot.StopPct, lot.RemindDate, lot.LeadDays, lot.Source,
		lot.PositionAccount, boolInt(lot.Detached), boolInt(lot.Enabled), lot.Note,
		writeTime(lot.CreatedAt), writeTime(lot.UpdatedAt))
	if err != nil {
		return fmt.Errorf("保存持仓批次失败: %w", err)
	}
	return nil
}

func (s *Store) ListLots(ctx context.Context, source string) ([]Lot, error) {
	query := `SELECT id,symbol,name,quantity,cost_price,buy_date,target_pct,stop_pct,remind_date,
		lead_days,source,position_account,detached,enabled,note,created_at,updated_at
		FROM monitor_lots`
	args := []any{}
	if source != "" {
		query += ` WHERE source=?`
		args = append(args, source)
	}
	query += ` ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lots := []Lot{}
	for rows.Next() {
		var lot Lot
		var detached, enabled int
		var created, updated string
		if err := rows.Scan(&lot.ID, &lot.Symbol, &lot.Name, &lot.Quantity, &lot.CostPrice,
			&lot.BuyDate, &lot.TargetPct, &lot.StopPct, &lot.RemindDate, &lot.LeadDays,
			&lot.Source, &lot.PositionAccount, &detached, &enabled, &lot.Note, &created, &updated); err != nil {
			return nil, err
		}
		lot.Detached = detached == 1
		lot.Enabled = enabled == 1
		lot.CreatedAt = parseTime(created)
		lot.UpdatedAt = parseTime(updated)
		lots = append(lots, lot)
	}
	return lots, rows.Err()
}

func (s *Store) DeleteLot(ctx context.Context, id string) error {
	// 事件与状态跟着批次走：留着它们只会让"删除后还冒消息"变成诡异的 bug。
	if _, err := s.db.ExecContext(ctx, `DELETE FROM monitor_lots WHERE id=?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM monitor_state WHERE state_key LIKE 'lot:'||?||'|%'`, id)
	return err
}

func (s *Store) GetLot(ctx context.Context, id string) (Lot, bool, error) {
	rows, err := s.ListLots(ctx, "")
	if err != nil {
		return Lot{}, false, err
	}
	for _, lot := range rows {
		if lot.ID == id {
			return lot, true, nil
		}
	}
	return Lot{}, false, nil
}

// ---------------------------------------------------------------- 引擎运行记录

func (s *Store) AppendRun(ctx context.Context, run Run) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO monitor_runs
		(task,started_at,finished_at,evaluated,triggered,error,duration_ms)
		VALUES (?,?,?,?,?,?,?)`,
		run.Task, writeTime(run.StartedAt), writeTime(run.FinishedAt),
		run.Evaluated, run.Triggered, run.Error, run.DurationMS)
	return err
}

type Run struct {
	ID         int64
	Task       string
	StartedAt  time.Time
	FinishedAt time.Time
	Evaluated  int
	Triggered  int
	Error      string
	DurationMS int64
}

func (s *Store) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,task,started_at,finished_at,evaluated,triggered,error,duration_ms
		FROM monitor_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []Run{}
	for rows.Next() {
		var run Run
		var started, finished string
		if err := rows.Scan(&run.ID, &run.Task, &started, &finished, &run.Evaluated,
			&run.Triggered, &run.Error, &run.DurationMS); err != nil {
			return nil, err
		}
		run.StartedAt = parseTime(started)
		run.FinishedAt = parseTime(finished)
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// Prune 清理过期数据：事件按日期、运行记录按小时。引擎每天收盘后调用一次。
func (s *Store) Prune(ctx context.Context, eventKeepDays, runKeepHours int, now time.Time) error {
	eventCutoff := now.AddDate(0, 0, -eventKeepDays).Format("2006-01-02")
	if _, err := s.db.ExecContext(ctx, `DELETE FROM monitor_events WHERE trade_date<? AND trade_date<>''`, eventCutoff); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM monitor_runs WHERE started_at<?`,
		writeTime(now.Add(-time.Duration(runKeepHours)*time.Hour)))
	return err
}

// ResetState 清空触发状态机：试算接口不需要它，但用户修完阈值后点一次
// 「重置状态」可以避免「旧状态卡住新阈值」的困惑。
func (s *Store) ResetState(ctx context.Context, ruleID string) error {
	if ruleID == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM monitor_state`)
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM monitor_state WHERE state_key LIKE ?`, ruleID+"|%")
	return err
}

// FailPendingDigest 把遗留的 pending 摘要置为 skipped：重启后没有人
// 能再 flush 它们，留成 pending 只会让列表出现「永远等不到通知」的行。
func (s *Store) FailPendingDigest(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE monitor_events SET notify_status=? WHERE notify_status=?`,
		NotifySkipped, NotifyPending)
	return err
}

// ---------------------------------------------------------------- 作用域快照

// SaveScopeSnapshot 记下某个作用域的清单。巡检组合只存在浏览器本地，
// 后台引擎读不到，必须由前端把「用户当前看到的持仓」推一份过来，
// 否则规则会按历史报告的老清单跑，和页面对不上。
func (s *Store) SaveScopeSnapshot(ctx context.Context, scope string, targets []ScopeTarget) error {
	payload, err := json.Marshal(targets)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO monitor_scope_snapshot (scope,payload,updated_at)
		VALUES (?,?,?)
		ON CONFLICT(scope) DO UPDATE SET payload=excluded.payload, updated_at=excluded.updated_at`,
		scope, string(payload), writeTime(utcNow()))
	return err
}

// LoadScopeSnapshot 读出作用域清单与同步时间；没同步过时返回空。
func (s *Store) LoadScopeSnapshot(ctx context.Context, scope string) ([]ScopeTarget, string, error) {
	var payload, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT payload,updated_at FROM monitor_scope_snapshot WHERE scope=?`, scope).
		Scan(&payload, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	targets := []ScopeTarget{}
	if err := json.Unmarshal([]byte(payload), &targets); err != nil {
		return nil, "", nil
	}
	return targets, updatedAt, nil
}

// ---------------------------------------------------------------- 关注仓

// WatchEntry 是监控关注仓里的一支标的。名字在加入时固定下来，
// 避免每次展示都要反查行情；后续快照命中时以快照为准。
type WatchEntry struct {
	Symbol    string     `json:"symbol"`
	Name      string     `json:"name,omitempty"`
	Note      string     `json:"note,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

// ListWatchlist 返回全部关注标的，按加入时间倒序（新加的在前面）。
func (s *Store) ListWatchlist(ctx context.Context) ([]WatchEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT symbol,name,note,created_at FROM monitor_watchlist ORDER BY created_at DESC, symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []WatchEntry{}
	for rows.Next() {
		var entry WatchEntry
		var created string
		if err := rows.Scan(&entry.Symbol, &entry.Name, &entry.Note, &created); err != nil {
			return nil, err
		}
		if parsed := parseTimeText(created); parsed != nil {
			entry.CreatedAt = parsed
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// AddWatchlist 加入一支标的；已存在时更新名称与备注（幂等）。
func (s *Store) AddWatchlist(ctx context.Context, symbol, name, note string) error {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return errors.New("标的代码不能为空")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO monitor_watchlist (symbol,name,note,created_at) VALUES (?,?,?,?)
		ON CONFLICT(symbol) DO UPDATE SET name=excluded.name,
			note=CASE WHEN excluded.note='' THEN monitor_watchlist.note ELSE excluded.note END`,
		symbol, strings.TrimSpace(name), strings.TrimSpace(note), writeTime(utcNow()))
	return err
}

// UpdateWatchlistNote 二次编辑备注：直接覆盖（空串即清空），
// 与 AddWatchlist 的「空备注不覆盖」upsert 语义刻意区分。
func (s *Store) UpdateWatchlistNote(ctx context.Context, symbol, note string) error {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return errors.New("标的代码不能为空")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE monitor_watchlist SET note=? WHERE symbol=?`,
		strings.TrimSpace(note), symbol)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return errors.New("关注仓里没有这只标的")
	}
	return nil
}

// RemoveWatchlist 移除一支标的；不存在也不报错（幂等）。
func (s *Store) RemoveWatchlist(ctx context.Context, symbol string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM monitor_watchlist WHERE symbol=?`, strings.TrimSpace(symbol))
	return err
}

// ClearWatchlist 清空关注仓。
func (s *Store) ClearWatchlist(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM monitor_watchlist`)
	return err
}
