# Inflection Recognition Engine V1

Related documentation:

- Chinese system design: [`inflection-system-design.md`](./inflection-system-design.md)
- Meeting-derived reasoning and assumptions: [`inflection-system-thinking.md`](./inflection-system-thinking.md)
- HTTP route catalog: [`api-routes.md`](./api-routes.md)

## Purpose

The engine translates the meeting's trading language into an explainable,
replayable rules model. It does not place orders and does not treat an anchor as
a buy recommendation.

The core relation is:

```text
old consensus anchor -> releases or loses control -> new carrier
                     \ environment anchor judges active/passive strength /
```

V1 evaluates one snapshot at a time. Minute collectors, persistence, T+1 fills,
and historical backtesting remain separate follow-up work.

## Anchor Kinds

| Kind | Meaning | Meeting analogue |
| --- | --- | --- |
| `old_profit` | The highest-recognition carrier of the old profit effect. | A former sentiment or trend leader. |
| `old_negative` | The highest-recognition carrier of old-cycle negative feedback. | A trapped, repeatedly limit-down old core. |
| `new_carrier` | A possible receiver of released liquidity. | The first active, recognizable new core. |

The engine selects the highest recognition candidate per kind. The selection is
clear only when the winner passes the configured recognition threshold and
leads the runner-up by the configured minimum gap. This prevents selecting a
"leader" after the fact when the top candidates are indistinguishable.

## Input Scales

The five fields inside `recognition` are normalized `0-100` values:

- `height`: stage height, limit-up height, or comparable position in the peer group.
- `attention`: amount, turnover, or other attention percentile.
- `persistence`: repeated strong days and continuation evidence.
- `influence`: leadership and peer-following evidence.
- `resilience`: relative strength during weak market or sector periods.

Raw returns remain percentage values. `amount_ratio` is current amount divided
by a comparable historical average. `breadth_improvement` is measured in
percentage points, while `limit_down_release_ratio` is a `0-1` fraction.

`prior_stress_score` preserves the environment immediately before a reversal.
It is needed because a current snapshot taken after breadth improves would
otherwise erase the severe stress that made a large inflection possible.

## Active and Passive Strength

The engine starts from a relative return:

```text
relative_return = stock_return - 0.5 * scope_return - 0.5 * market_return
```

Active weakness combines negative relative return, distance below VWAP, broken
or limit-down state, failure versus the stored expectation, and selling with
meaningful amount. A stock dragged down by a much weaker market can therefore
remain relatively strong and should not automatically invalidate the old
anchor.

Active strength combines positive relative return, distance above VWAP,
limit-up confirmation, sector followers, and sufficient amount.

## Big Inflection

V1 combines four factors:

1. severe prior market stress;
2. clearance of the strongest old positive or negative anchor;
3. environment transition from deterioration to improvement;
4. active strength of a clear new carrier.

A high weighted score alone produces only `candidate`. `confirmed` additionally
requires every gate to pass and the new carrier anchor to be unambiguous. The
result explicitly warns that a candidate is an anticipated inflection rather
than an inflection that has already happened.

## Small Inflection

V1 supports two setups:

- `high_low_switch`: a clear old profit anchor actively weakens while a new or
  lower-position carrier becomes active;
- `individual_reversal`: a candidate that was previously passively weakened by
  the environment now actively reverses relative to that environment.

The result always warns that a small inflection is a local early attempt and
does not imply a new market cycle.

## Example Request

```json
{
  "market": {
    "scope": "sector",
    "market_change_percent": 0.8,
    "scope_change_percent": 1.2,
    "advancers": 2800,
    "decliners": 2000,
    "limit_ups": 36,
    "limit_downs": 12,
    "broken_boards": 15,
    "previous_limit_up_premium": -0.8,
    "stress_days": 1,
    "breadth_improvement": 12,
    "index_above_vwap": true
  },
  "candidates": [
    {
      "symbol": "603137.SH",
      "name": "old sentiment core",
      "kind": "old_profit",
      "scope": "sector",
      "recognition": {
        "height": 96,
        "attention": 90,
        "persistence": 92,
        "influence": 90,
        "resilience": 70
      },
      "change_percent": -5.5,
      "scope_change_percent": 1.2,
      "market_change_percent": 0.8,
      "vwap_distance_percent": -3.2,
      "expectation_gap_percent": -6,
      "amount_ratio": 1.5,
      "board_broken": true
    },
    {
      "symbol": "000676.SZ",
      "name": "new low-position carrier",
      "kind": "new_carrier",
      "scope": "sector",
      "recognition": {
        "height": 78,
        "attention": 82,
        "persistence": 72,
        "influence": 75,
        "resilience": 80
      },
      "change_percent": 9.9,
      "scope_change_percent": 1.2,
      "market_change_percent": 0.8,
      "vwap_distance_percent": 3,
      "amount_ratio": 1.6,
      "sector_followers": 3,
      "limit_up": true
    }
  ]
}
```

The response returns market stress, environment turn score, all candidate
scores, selected anchors, big and small signal evaluations, factor evidence,
risks, and `primary_signal`.

## Current Boundaries

- Default weights and thresholds are configurable starting assumptions, not
  validated profitability claims.
- Snapshot evaluation does not model same-minute fills, one-line limit-ups,
  T+1, limit-down exits, slippage, fees, or capacity.
- A real signal service still needs minute bars, full-market breadth, limit-down
  release events, previous-limit-up premium, and stored expectation snapshots.
- Historical calibration must keep all failed signals and use walk-forward
  validation rather than fitting the full sample.
