<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/desktop/assets/easy-stock.png" width="112" height="112" alt="easy-stock Logo" />
</p>

<h1 align="center">easy-stock: An AI Research Workbench for the China A-Share Market</h1>

<p align="center"><strong>A local-first desktop app for A-share market analysis, stock research, and AI-powered review — built for individual investors</strong></p>

<p align="center"><sub>English | <a href="./README.md">简体中文</a></sub></p>

<p align="center">
  Let AI actually understand the market — and make every judgment traceable to evidence.<br />
  Turn intraday observation, post-market review, and long-term insight into one continuously evolving research system.
</p>

<p align="center">
  <a href="https://github.com/jundizhou/easy-stock/releases/latest"><strong>Download</strong></a> ·
  <a href="#why-easy-stock">Why</a> ·
  <a href="#core-features">Core Features</a> ·
  <a href="#how-ai-augments-a-share-research">How AI Helps</a> ·
  <a href="#ai-native-architecture">Architecture</a> ·
  <a href="#getting-started">Getting Started</a> ·
  <a href="https://github.com/jundizhou/easy-stock/discussions">Discussions</a> ·
  <a href="./CONTRIBUTING.md">Contributing</a> ·
  <a href="#license">License</a>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Backend-Go-00ADD8?logo=go&logoColor=white" />
  <img alt="React" src="https://img.shields.io/badge/Frontend-React%20%2B%20TypeScript-3178C6?logo=react&logoColor=white" />
  <img alt="Electron" src="https://img.shields.io/badge/Desktop-Electron-47848F?logo=electron&logoColor=white" />
  <img alt="Hermes" src="https://img.shields.io/badge/AI-Hermes-6D5BD0" />
  <img alt="Local First" src="https://img.shields.io/badge/Data-Local%20First-159A80" />
  <img alt="License" src="https://img.shields.io/badge/License-Non--Commercial-EA580C" />
  <a href="https://github.com/jundizhou/easy-stock/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/jundizhou/easy-stock?label=Release" /></a>
  <a href="https://github.com/jundizhou/easy-stock/stargazers"><img alt="GitHub stars" src="https://img.shields.io/github/stars/jundizhou/easy-stock?style=flat" /></a>
</p>

<p align="center">
  <a href="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-product-overview.png">
    <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-product-overview.png" width="1280" alt="easy-stock product overview: trend themes, leading-stock ladder, daily K-line and leader analysis" />
  </a>
</p>

<p align="center"><sub>Market Overview · Automated Review Digest · Theme Radar · Sentiment & Limit-Up Ladder · AI Stock Analysis · Portfolio Inspection · Local Research Memory</sub></p>

---

## Why easy-stock

An AI-era stock terminal shouldn't just bolt a chat box onto a legacy quote app.

Traditional market software is good at showing prices, changes, and turnover. What is actually hard about A-share trading is understanding the structure behind the numbers: What is today's main theme? Is the theme broadening? Did the limit-up ladder extend higher? Did yesterday's strongest stocks get follow-through? Is the trend still intact? Is market sentiment repairing or fading?

easy-stock is an AI-native workbench that speaks this language. It unifies quotes, themes, limit-up pools, news, and post-market reviews into a single local-first research pipeline:

| AI-native capability | How easy-stock does it |
| --- | --- |
| **Sense the market** | Aggregates Kaipanla, Eastmoney, Sina, Cailianpress and other sources into one unified model of quotes, K-lines, themes, limit-ups, and news |
| **Understand structure** | Turns trends, themes, limit-up ladders, sentiment, volume/price, and relative strength into a computable domain model |
| **Execute tasks** | Scheduled jobs, a built-in browser, and agents that discover articles, deduplicate, archive, and distill opinions |
| **Build memory** | Keeps articles, summaries, sentiment history, analysis records, model sessions, and research caches on your machine |
| **Verify conclusions** | Preserves scoring dimensions, theme sources, original URLs, timestamps, and degradation state — every conclusion can be traced back to evidence |

> easy-stock does not make decisions for you. It gives you better perception, faster research, and a judgment system that compounds over time.

> **Note:** The app UI and docs are currently Chinese-first (the product targets the China A-share market, where data sources and market microstructure are highly localized). English localization is on the [roadmap](./ROADMAP.md); the codebase, architecture, and this README are English-friendly.

---

## Core Features

### 01 · Automated Post-Market Review Digest

#### Let AI collect and organize what multiple market writers actually said

Post-market reviews from well-known A-share commentators are scattered across Xueqiu, TaoGuba, and WeChat. Manually visiting each profile, filtering for today's articles, and copying text is a time sink. easy-stock organizes all of it into a unified review timeline:

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-auto-review.png" width="2560" height="1692" alt="easy-stock automated review digest and daily sync workbench" />
</p>

Once collection finishes, one click generates a "today's consensus" report — distilling shared focus areas, key disagreements, market facts, and conditions to verify in the next session:

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-ai-daily-consensus.png" width="2560" height="1692" alt="easy-stock AI consensus of commentator views and next-session expectations" />
</p>

The goal is not "AI guessing what rises tomorrow" — it is turning dozens of unstructured articles into a readable, verifiable opinion map you can keep tracking.

### 02 · Limit-Up Ladder & Sentiment Cycle Analysis

#### See the ladder, the promotion rate, and where you are in the sentiment cycle

A-shares have a ±10% daily price limit, so reading the limit-up ladder (how many stocks sealed the limit, how many consecutive days, which rung failed) is central to Chinese short-term trading. easy-stock puts the limit-up pool, consecutive limit-up ladder, yesterday's follow-through, promotion rates, and sentiment history in one view, with a built-in algorithm for sentiment-cycle staging:

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-short-term-analysis.png" width="2560" height="1692" alt="easy-stock limit-up ladder, market sentiment and promotion structure analysis" />
</p>

### 03 · Theme Radar

#### Identify the market's real leading themes from sector moves

Aggregates theme rankings, momentum strength, money flow, market breadth, and streak duration; breaks down industry chains, concept nodes, and sub-directions through a theme map; and combines AI reads of the market trend, theme stage, and conditional entry points for trend stocks.

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-theme-radar.png" width="2560" height="1696" alt="easy-stock theme radar, mainline heat and stock ladder analysis" />
</p>

### 04 · Market Overview

#### One traceable map of indices, money flow, leaderboards, and research signals

Core indices, news flashes, industry trend strength, sector money flow, themes, per-stock inflows/outflows, the Dragon-Tiger list, announcement radar, institutional views, and industry research — unified into a single research entry point. Every page keeps its data source and fetch time, and you can hand the current context straight to AI for interpretation, instead of hopping between quote terminals and news pages.

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-market-overview-indices.png" width="2560" height="1696" alt="easy-stock market overview with core indices and cross-market trend analysis" />
</p>

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-market-overview-research.png" width="2560" height="1696" alt="easy-stock market overview with institutional views and research report evidence" />
</p>

### 05 · AI Stock Analysis

#### One explainable decision system for both trend stocks and sentiment stocks

For any stock, the engine first classifies the likely path — sentiment ladder, trend capacity, trend growth, range-bound watch, or weak/risk — then weights each path and blends your own trading experience, review notes, and short-term trading wisdom into a personalized stock report.

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-stock-ai-analysis-overview.png" width="2560" height="1696" alt="easy-stock AI stock analysis overview with multi-dimensional scoring and theme positioning" />
</p>

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-stock-ai-analysis-evidence.png" width="2560" height="1696" alt="easy-stock AI stock analysis with conditional decisions, fact chains and risk boundaries" />
</p>

<p align="center"><sub>Screenshots show page structure; stock states, theme tags, scores, and risk parameters change dynamically with trading days, cached snapshots, and data source availability.</sub></p>

### 06 · Portfolio AI Inspection

#### From single-stock calls to portfolio-level checks

Choose an aggressive, balanced, or steady trading style, add up to 10 holdings by name or code, and set position weights on a slider (the remainder is treated as cash). The engine then runs the full per-stock analysis in the background and produces a portfolio report.

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-portfolio-inspection-setup.png" width="2560" height="1688" alt="easy-stock portfolio inspection setup: trading style, stock search and weight configuration" />
</p>

The report uses a deterministic health score, broken down into stock quality, risk resilience, diversification, and style fit — then flags theme concentration, pairwise correlation, stop-loss risk, and risk contribution. Each stock gets an action, a confirmation condition, and an invalidation condition; stocks with a completed analysis can jump straight to their full report without another AI call.

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-portfolio-inspection-report.png" width="2560" height="1688" alt="easy-stock portfolio inspection report: health score, key risks, concentration and per-stock actions" />
</p>

<p align="center"><sub>Portfolio inspection is for research and risk awareness; health scores, risk contributions, and action conditions change with the market, your configuration, and data coverage.</sub></p>

### 07 · Trading Wisdom Library & AI Copilot

#### Turn hard-won trading experience into durable, re-readable knowledge — and let AI learn how you research

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-trading-mastery.png" width="2560" height="1696" alt="easy-stock trading wisdom library, trader profiles and Hermes deep reading" />
</p>

### The Daily Research Loop

| Stage | What easy-stock provides |
| --- | --- |
| **Intraday discovery** | Market overview, trend themes, live quotes, theme map, limit-up ladder, and data source status |
| **Stock research** | Path classification, multi-timeframe trends, relative strength, theme attribution, next-day scenarios, and risk boundaries |
| **Portfolio inspection** | Style matching, portfolio health, concentration and correlation risks, per-stock actions, and links to full reports |
| **Post-market review** | Sentiment timeline, yesterday's follow-through, promotion structure, and limit-up ladder analysis |
| **Information gathering** | Automatic profile sync from commentators, article import, text cleaning, and local archiving |
| **AI distillation** | Per-article summaries, per-author synthesis, cross-author consensus, disagreements, and next-session watch conditions |
| **Long-term accumulation** | Article library, trading wisdom, stock analysis records, AI sessions, and local research memory |

---

## How AI Augments A-Share Research

### 1. From "looking at data" to "understanding market structure"

easy-stock first organizes themes, ladders, limit-up reasons, trends, relative strength, and sentiment history into a domain model — then hands that structure to AI. The model never faces isolated numbers; it faces market evidence with A-share semantics.

### 2. From "manual browsing" to "agents that act"

Hermes drives agent-browser and a persistent Electron browser session: following your subscriptions, it visits Xueqiu or TaoGuba profiles, discovers new articles, identifies author and publish time, deduplicates, extracts full text, archives, and distills — automatically.

### 3. From "single summaries" to "an opinion network"

The system first synthesizes each author's core views, then aggregates across authors for the day's consensus and disagreements — separating market facts, minority expectations, shared focus, and conditions to verify next session.

### 4. From "generic chat" to "an A-share research copilot"

All AI sessions run through the local Hermes Runtime, which handles model calls, session continuation, tool routing, skills, and task context — business code is never locked to one vendor. OpenAI, DeepSeek, Qwen, Moonshot, Anthropic, and any OpenAI-compatible endpoint are supported.

### 5. From "one-off Q&A" to "a long-term research flywheel"

| Perceive | Understand | Act | Remember | Verify |
| --- | --- | --- | --- | --- |
| Live quotes, themes, articles | AI identifies structure, views, disagreements | Auto-sync, analyze, draft plans | Local articles, history, sessions | Verify next session against prices and sources |

Every verification becomes context for the next round of research. Over time the system accumulates not just more data, but a research process and a body of judgment that fits the way you work.

---

## AI-Native Architecture

<p align="center">
  <img src="https://cdn.jsdelivr.net/gh/jundizhou/easy-stock@main/docs/assets/easy-stock-ai-architecture.svg" width="1680" height="1180" alt="easy-stock AI-native architecture diagram" />
</p>

| Layer | Responsibility |
| --- | --- |
| **AI research experience** | Market overview, trend themes, limit-up ladder, AI stock analysis, portfolio inspection, trading wisdom, review digest, AI copilot |
| **Business orchestration** | Go API, stock analysis engine, portfolio inspection engine, strategy evaluation, review intelligence, scheduled jobs, data source status |
| **AI agent platform** | Hermes prompts, skills, sessions, memory, tool router, and an open model gateway |
| **Data intelligence** | Provider registry, unified market model, theme attribution, evidence metadata, SQLite, cached snapshots |
| **External ecosystem** | Market data sources, content platforms, browser execution, and the LLM services you choose |
| **Desktop runtime boundary** | Electron lifecycle, random ports, one-time tokens, persistent browser sessions, local asset assembly |
| **Security & governance** | Key isolation, source tracking, graceful degradation, runtime status, risk notices |

---

## Getting Started

### Users

No Node.js, Go, or Python required. Grab the installer or archive for your OS from [GitHub Releases](https://github.com/jundizhou/easy-stock/releases/latest).

For first-time setup (LLM API keys, Xueqiu/TaoGuba login for content sync), see the [User Guide](./docs/user-guide.md) (Chinese). To add or enable local skills, see the [Skill Installation Guide](./docs/skill-installation.md) (Chinese).

### Developers

To run, debug, test, or build from source, see the [Developer Guide](./docs/development.md) (Chinese — architecture diagrams and code are language-neutral).

## Community & Contributing

- Discussions, research methods, and use cases: [GitHub Discussions](https://github.com/jundizhou/easy-stock/discussions)
- Bugs and data issues: [open an issue](https://github.com/jundizhou/easy-stock/issues/new/choose) with your OS, app version, and reproduction steps
- Feature requests: describe the scenario and the outcome you expect
- Code and docs contributions: start with the [Contributing Guide](./CONTRIBUTING.md) and the [Roadmap](./ROADMAP.md)
- Chinese-speaking chat: [QQ group 422158208](https://qm.qq.com/q/lizlauc32U)
- Security issues: follow the [Security Policy](./SECURITY.md) and report privately

---

## License

The original backend, frontend, desktop app, and documentation of easy-stock are licensed under the [PolyForm Noncommercial License 1.0.0](./LICENSE).

- Personal learning, research, experimentation, and other non-commercial use, modification, and distribution are allowed, provided the license and copyright notice are retained.
- Any direct or indirect commercial use — including production deployment, paid services, SaaS, paid consulting or training, integration into commercial products, or resale — requires separate written permission from the author.
- Third-party dependencies, data sources, and bundled materials remain under their original licenses and terms of service.
- easy-stock is source-available software, not OSI open source.

---

## Disclaimer

> This project is for learning, research, and information organization only. It is not investment advice, a promise of returns, or a trading basis. Markets carry risk, and AI output and third-party data may be delayed, incomplete, or wrong — always verify against original sources, judge independently, and own your decisions.

<p align="center"><sub>Local first · Evidence based · Human in control</sub></p>
