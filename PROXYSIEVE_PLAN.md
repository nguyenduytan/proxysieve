# ProxySieve v1.0 — Full Project & Codex Implementation Plan

> **Tagline:** Smart traffic control for any proxy.  
> **Subheadline:** Stop paying for bytes you don't need. Route smarter, filter earlier, measure everything.

**Plan status:** Implementation contract for Codex  
**Target release:** `v1.0.0` — Full Community Edition  
**Primary language:** Go 1.27.x  
**Dashboard:** React 19.3 + TypeScript + Vite 8.1  
**Frontend runtime/tooling:** Node.js 24 LTS  
**Default database:** SQLite  
**License recommendation:** Apache-2.0  
**Default deployment:** Single self-contained binary + Docker image  
**Supported OS targets:** Linux, Windows, macOS  
**Default security posture:** localhost-only, non-MITM, least privilege, secrets redacted

---

## 0. Purpose of this document

This file is the source-of-truth implementation plan for Codex and future contributors. It is intentionally more detailed than a normal roadmap. Codex must treat it as a technical contract, not as a brainstorm.

The goal is to build **ProxySieve**, a modular, self-hosted proxy traffic control plane that sits between clients and upstream residential/datacenter/rotating proxies. It must reduce unnecessary paid proxy traffic, improve proxy quality and routing decisions, expose strong observability and administration, and remain easy to fork and extend.

ProxySieve is **not** just an image blocker, a browser extension, a proxy list checker, or a thin wrapper around one provider. The v1.0 product must be useful as an independent gateway for browsers, automation frameworks, scripts, CLI tools, backend services, and other applications that understand HTTP/SOCKS proxies.

This plan deliberately targets a **feature-complete v1.0**, while still requiring Codex to implement the product in small, testable milestones. Do not collapse the work into one giant coding pass.

---

# Part I — GitHub Repository Creation & Initial Setup

## 1. Recommended repository identity

### 1.1 Repository name

Use:

```text
proxysieve
```

Display/product name:

```text
ProxySieve
```

Recommended GitHub description:

```text
Smart traffic control for paid proxies — filter waste, route intelligently, manage sessions, measure bandwidth and cost.
```

Recommended GitHub topics:

```text
proxy
proxy-server
proxy-manager
http-proxy
socks5
residential-proxy
datacenter-proxy
rotating-proxy
traffic-filter
bandwidth
web-scraping
playwright
puppeteer
selenium
golang
self-hosted
networking
```

Do not include a provider brand in the repository name or project identity. Provider integrations must remain adapters.

### 1.2 Visibility

Recommended: **Public repository** from the beginning if the intention is open source and community growth.

Reasons:

- easier discovery and stars;
- GitHub public repository security tooling is broadly available;
- contributors can fork immediately;
- GitHub Discussions and Issues can become the community support surface;
- architectural transparency is important for a network/security tool.

If initial development must stay private, keep the same structure and make it public before the first public release. Never commit real proxy credentials even in a private repository.

### 1.3 License

Recommended: **Apache License 2.0**.

Why:

- permissive and fork-friendly;
- explicit patent grant is useful for infrastructure/networking projects;
- commercial and private use remain possible;
- encourages provider/community extensions.

If maximum simplicity is preferred over the patent clause, MIT is acceptable, but the default plan should use Apache-2.0.

---

## 2. Recommended repository bootstrap workflow

Prefer building the initial scaffold locally and then pushing to an empty GitHub repository. This avoids merge noise from a GitHub-generated README/license while Codex is creating the canonical structure.

### 2.1 Create repository locally

Example commands:

```bash
mkdir proxysieve
cd proxysieve

git init -b main

go mod init github.com/<OWNER>/proxysieve

mkdir -p cmd/proxysieve
mkdir -p pkg
mkdir -p internal
mkdir -p web
mkdir -p integrations
mkdir -p extensions/examples
mkdir -p examples
mkdir -p configs
mkdir -p docs
mkdir -p benchmarks
mkdir -p test
mkdir -p tools
mkdir -p .github/workflows
mkdir -p .github/ISSUE_TEMPLATE
```

Replace `<OWNER>` with the final GitHub username or organization before publishing public package import paths.

### 2.2 Initial files that must exist before first push

Create at minimum:

```text
README.md
PLAN.md
LICENSE
NOTICE
SECURITY.md
CONTRIBUTING.md
CODE_OF_CONDUCT.md
SUPPORT.md
CHANGELOG.md
CODEOWNERS
.gitignore
.gitattributes
.editorconfig
.golangci.yml
.goreleaser.yml
Dockerfile
docker-compose.yml
Makefile
go.mod
go.sum
package.json or web/package.json
pnpm-lock.yaml
.env.example
config.example.yaml
.github/dependabot.yml
.github/pull_request_template.md
.github/ISSUE_TEMPLATE/bug.yml
.github/ISSUE_TEMPLATE/feature.yml
.github/ISSUE_TEMPLATE/provider.yml
.github/ISSUE_TEMPLATE/config.yml
```

`PLAN.md` in the repository should be this document or a normalized copy of it.

### 2.3 First commit

Use Conventional Commits from the first commit:

```bash
git add .
git commit -m "chore: bootstrap ProxySieve repository"
```

### 2.4 Create GitHub repository with GitHub CLI

Recommended if `gh` is available:

```bash
gh repo create <OWNER>/proxysieve \
  --public \
  --source=. \
  --remote=origin \
  --push \
  --description "Smart traffic control for paid proxies — filter waste, route intelligently, manage sessions, measure bandwidth and cost."
```

Then configure repository metadata:

```bash
gh repo edit <OWNER>/proxysieve \
  --enable-issues \
  --enable-discussions \
  --enable-projects \
  --delete-branch-on-merge \
  --allow-squash-merge \
  --allow-merge-commit=false \
  --allow-rebase-merge=false
```

Recommended merge strategy for this project: **Squash merge only**. This keeps `main` readable while contributors can use as many local commits as necessary.

If `gh` is not installed, perform equivalent settings in GitHub Settings.

---

## 3. GitHub branch and ruleset strategy

GitHub Rulesets should be used as the primary protection mechanism for `main`. They provide clearer visibility and can combine multiple rules.

### 3.1 Branch model

Use a simple trunk-based contribution model:

```text
main
  ├── feat/<short-name>
  ├── fix/<short-name>
  ├── perf/<short-name>
  ├── refactor/<short-name>
  ├── docs/<short-name>
  ├── test/<short-name>
  └── chore/<short-name>
```

Do **not** create a permanent `develop` branch unless a later release process demonstrates a real need. `main` should always remain releasable.

Release work happens through tags:

```text
v0.1.0
v0.2.0
...
v1.0.0
```

### 3.2 Main branch ruleset

Create an active ruleset named:

```text
Protect main
```

Target:

```text
main
```

Required protections:

- prevent branch deletion;
- block force pushes;
- require a pull request before merging;
- require conversation resolution before merge;
- require linear history;
- require required status checks;
- require branch to be up to date before merge once CI is stable;
- optionally require signed commits later if it does not hurt contribution UX;
- require code scanning results once CodeQL is enabled;
- do not give broad bypass permissions to bots.

For a solo-maintainer bootstrap, the owner/admin may have a bypass path so early repository setup is not deadlocked. Once maintainers grow, tighten bypass permissions.

### 3.3 Required checks

After the first CI workflows have run at least once, add these required checks to the ruleset:

```text
lint-go
unit-go
race-go
build-go
frontend-lint
frontend-test
frontend-build
integration
security
```

Do not require a check name before GitHub has observed that check, otherwise the first bootstrap PR may be blocked unnecessarily.

For later milestones add:

```text
e2e-playwright
e2e-puppeteer
protocol-matrix
migration-test
```

### 3.4 Pull request review policy

Initial public stage:

- PR required;
- owner can merge through maintainer bypass when working solo;
- once a second maintainer exists, require 1 approval;
- sensitive modules should use CODEOWNERS.

Sensitive ownership patterns:

```text
/pkg/gateway/          @<OWNER>
/pkg/proxy/            @<OWNER>
/pkg/policy/           @<OWNER>
/internal/security/    @<OWNER>
/internal/inspect/     @<OWNER>
/.github/workflows/    @<OWNER>
```

Update CODEOWNERS when more maintainers join.

---

## 4. GitHub security settings

Enable from the beginning where the repository/plan supports them:

### 4.1 Dependabot

Enable:

- Dependabot alerts;
- Dependabot security updates;
- Dependabot version updates.

`.github/dependabot.yml` should cover:

- Go modules;
- npm/pnpm package ecosystem;
- GitHub Actions;
- Docker when useful.

Prefer weekly grouped dependency updates instead of dozens of tiny daily PRs.

### 4.2 CodeQL

Enable CodeQL default setup first. This project contains Go and TypeScript/JavaScript, both suitable for code scanning.

Start with the default query suite. Move to `security-extended` after baseline false positives and build behavior are understood.

### 4.3 Secret scanning and push protection

Enable secret scanning/push protection when available for the repository. ProxySieve is especially exposed to accidental credential commits because contributors will test real proxies.

The repository must also implement local safeguards:

- `.env` ignored;
- `*.secrets.*` ignored;
- fixtures use fake credentials;
- logs redact secrets;
- examples use `username:password@example.invalid`;
- documentation explicitly warns users not to paste real credentials into GitHub issues.

### 4.4 GitHub Actions permissions

Repository Actions settings should use restricted/default read permissions where practical.

Every workflow must explicitly set least privilege, normally:

```yaml
permissions:
  contents: read
```

Release workflows may request narrowly scoped write permissions only where needed.

Avoid `pull_request_target` unless the security implications are understood and the workflow is specifically designed for untrusted forks.

---

## 5. GitHub collaboration surfaces

### 5.1 Issues

Use GitHub Issue Forms, not blank free-form issue templates for common reports.

Create:

```text
Bug Report
Feature Request
Proxy Provider Integration
Performance / Benchmark Issue
Documentation Issue
```

Each bug report should request:

- ProxySieve version;
- OS/architecture;
- deployment mode;
- proxy protocol;
- downstream client;
- sanitized config;
- sanitized logs;
- reproduction steps;
- expected vs actual behavior.

Never request real credentials.

### 5.2 Discussions

Enable Discussions and use categories:

```text
Announcements
General
Q&A
Show and Tell
Providers & Integrations
Ideas
Benchmarks
```

Use Discussions for support/design questions; keep Issues for actionable defects/features.

### 5.3 Labels

Create consistent labels:

```text
type:bug
type:feature
type:docs
type:security
type:performance
type:provider
type:integration
type:refactor

area:gateway
area:proxy
area:policy
area:sessions
area:traffic
area:health
area:cache
area:inspect
area:api
area:web
area:cli
area:security
area:ci

priority:p0
priority:p1
priority:p2
priority:p3

good first issue
help wanted
breaking-change
blocked
needs-triage
```

### 5.4 Milestones

Create GitHub milestones matching implementation milestones in this plan. Do not use arbitrary date promises unless maintainers actually commit to those dates.

---

# Part II — Product Vision & Architectural Principles

## 6. Product definition

ProxySieve is a programmable traffic gateway that receives proxy traffic from a client, evaluates policies, chooses an action, optionally selects an upstream proxy and session, accounts for traffic/cost, and returns the result to the client.

Primary flow:

```text
Client
  |
  v
ProxySieve downstream listener
  |
  v
Authentication / normalization
  |
  v
Policy pipeline
  |
  +--> BLOCK
  +--> REJECT
  +--> DIRECT
  +--> CACHE
  +--> MOCK
  +--> THROTTLE
  +--> PROXY
          |
          v
     Session Resolver
          |
          v
      Pool Resolver
          |
          v
     Proxy Selector
          |
          v
     Health / Budget Guard
          |
          v
     Upstream Proxy
          |
          v
       Internet
```

Response processing:

```text
Internet / Upstream
       |
       v
Response policy
       |
       v
Optional cache
       |
       v
Accounting / metrics / events
       |
       v
Client
```

---

## 7. Non-negotiable architectural principles

Codex must preserve these principles throughout implementation.

### 7.1 Protocol engine must not depend on provider brands

Forbidden:

```go
if provider == "some-vendor" { ... }
```

inside generic gateway, policy, routing, session or traffic modules.

Provider-specific behavior belongs in provider adapters.

### 7.2 UI must consume public API

The React dashboard must never access SQLite or backend storage directly. All control-plane behavior goes through versioned API/service interfaces.

### 7.3 Storage must be replaceable

SQLite is the default implementation, not the architecture. Core modules depend on repository/store interfaces.

### 7.4 Extensions must not import `internal/`

Anything intended for third-party reuse must live under stable public packages or versioned extension contracts.

### 7.5 Safe defaults

Default behavior must be:

- listen on loopback only;
- no open proxy;
- no HTTPS interception;
- no direct route that leaks host IP;
- metadata-only logging;
- secrets redacted;
- private/link-local destinations denied to untrusted clients;
- outbound timeouts enabled;
- bounded queues and cache sizes.

### 7.6 One-binary experience

The default installation should not require PostgreSQL, Redis, NATS, Docker Compose, or Kubernetes.

Expected basic UX:

```bash
proxysieve start
```

Advanced external services may be supported through adapters later without becoming hard dependencies.

### 7.7 Modular full v1

Feature-complete does not mean monolithic. Every large feature must be behind an interface/service boundary and, where appropriate, a feature flag.

### 7.8 Honest accounting

Actual measured upstream bytes and estimated avoided bytes are different metrics. Never report estimated savings as exact measured savings.

---

## 8. v1.0 high-level feature scope

ProxySieve v1.0 must include:

- HTTP forward proxy listener;
- HTTPS CONNECT tunneling;
- SOCKS5 listener;
- HTTP/HTTPS/SOCKS5 upstream proxies;
- static and rotating proxies;
- proxy lists and API-fed proxy sources;
- proxy normalization;
- proxy pools;
- proxy chaining;
- sticky sessions;
- smart proxy selection;
- health scoring;
- circuit breaker;
- smart retry/failover;
- policy/rule engine;
- block/proxy/direct/cache/throttle/reject/mock/redirect/rewrite actions where technically applicable;
- traffic accounting;
- configurable cost accounting;
- budgets and limits;
- DNS cache;
- response cache;
- shadow policies;
- browser-aware filtering adapters;
- Playwright integration;
- Puppeteer integration;
- Selenium integration foundation;
- optional HTTPS inspect mode;
- admin REST API;
- realtime event transport;
- CLI;
- admin dashboard;
- user authentication;
- RBAC roles;
- API keys for clients;
- events and webhooks;
- alert engine;
- import/export;
- backup/restore;
- extension contracts;
- provider SDK/examples;
- metrics and traces;
- Prometheus-compatible metrics;
- OpenTelemetry hooks;
- Docker image;
- cross-platform binaries;
- migrations;
- CI, security scanning, tests and benchmarks;
- complete documentation.

---

## 9. Explicit non-goals for v1 core

Even with a full-featured v1, avoid unrelated scope expansion. The following are not required to call v1 complete:

- full VPN/TUN device implementation;
- mobile iOS/Android VPN apps;
- Kubernetes Operator;
- multi-region distributed consensus;
- hosted SaaS billing platform;
- proxy marketplace;
- machine-learning proxy selector;
- AI-generated policies;
- browser fingerprint spoofing;
- CAPTCHA solving;
- mechanisms whose primary purpose is bypassing third-party access controls;
- proprietary provider credentials embedded in source.

The architecture may leave extension points for some infrastructure capabilities, but Codex must not derail v1 implementation to build them.

---

# Part III — Technology Baseline

## 10. Backend stack

Use:

```text
Go 1.27.x
```

Requirements:

- `go.mod` should declare the chosen Go baseline;
- use standard library where practical;
- keep external dependencies focused and auditable;
- run `go vet` and `go test -race` in CI;
- use `context.Context` consistently for cancellation/deadlines;
- avoid unbounded goroutine creation;
- use atomic/counter primitives carefully for hot-path accounting;
- avoid reflection-heavy dependency injection frameworks.

Recommended architectural style: pragmatic ports-and-adapters / hexagonal architecture, not ceremony-heavy “Clean Architecture”.

Use interfaces at actual boundaries. Do not create an interface for every struct.

---

## 11. Frontend stack

Use:

```text
React 19.3
TypeScript
Vite 8.1
Node.js 24 LTS for development/CI
pnpm
```

Frontend requirements:

- SPA admin panel;
- generated or strongly typed API client from OpenAPI where practical;
- responsive desktop-first layout;
- dark/light theme;
- accessible forms/tables/navigation;
- virtualized large request tables where necessary;
- realtime views via SSE by default; WebSocket only where bidirectional realtime behavior is needed;
- production assets embedded into the Go binary where possible.

Do not use Next.js unless a future requirement genuinely needs server-side rendering. This is an admin application, not an SEO website.

---

## 12. Persistence stack

Default:

```text
SQLite
```

Also provide:

```text
in-memory repositories for tests and ephemeral mode
```

Database requirements:

- WAL mode where appropriate;
- prepared statements;
- transaction boundaries at service level;
- versioned SQL migrations;
- migration tests;
- indexes for high-volume lookup tables;
- retention/compaction for traffic data;
- no secrets stored in plaintext where encryption is configured/required.

Do not require Redis for v1. Internal bounded in-memory caches are sufficient for the single-node default deployment.

---

## 13. API baseline

Use versioned REST API:

```text
/api/v1
```

Use OpenAPI 3.x as the API contract.

Realtime stream:

```text
/api/v1/events/stream
```

Prefer SSE for server-to-dashboard event feeds because it is simple and firewall-friendly. Use WebSocket only for features that truly require client-to-server realtime messaging.

---

# Part IV — Domain Model & Core Modules

## 14. Core domain entities

Codex must define stable domain types before implementing protocol details.

### 14.1 ProxyEndpoint

Represents a usable upstream endpoint after normalization.

Suggested fields:

```text
ID
Name
Protocol
Host
Port
UsernameRef / credential reference
ProviderID
SourceID
PoolIDs
Tags
Country
Region
City
ASN
ISP
CostPerGB
Priority
Weight
Enabled
CreatedAt
UpdatedAt
Metadata
```

Do not make provider-specific username syntax part of the generic type.

### 14.2 ProxySource

Represents where proxy endpoints originate:

```text
manual
file
URL/API
provider adapter
generated rotating gateway
```

Suggested fields:

```text
ID
Name
Type
ProviderID
RefreshInterval
LastRefreshAt
LastRefreshStatus
Config
Enabled
```

### 14.3 ProxyProvider

Provider abstraction for provider-specific operations.

Interface responsibilities may include:

```go
type Provider interface {
    ID() string
    DisplayName() string
    Capabilities() ProviderCapabilities
    ValidateConfig(ctx context.Context, cfg ProviderConfig) error
    FetchEndpoints(ctx context.Context, cfg ProviderConfig) ([]EndpointCandidate, error)
    BuildCredential(ctx context.Context, req CredentialRequest) (CredentialMaterial, error)
    HealthMetadata(ctx context.Context, endpoint ProxyEndpoint) (ProviderHealthMetadata, error)
}
```

Optional interfaces should be split for optional capabilities instead of one massive interface, for example:

```text
EndpointFetcher
CredentialBuilder
QuotaReader
GeoResolver
SessionRotator
```

### 14.4 ProxyPool

Logical group used for routing.

Fields:

```text
ID
Name
Description
Strategy
FallbackPoolIDs
CountryConstraint
TagConstraints
MinHealthScore
MaxLatency
CostPreference
SessionPolicy
Enabled
```

### 14.5 ProxySession

Fields:

```text
ID
ClientID
Key
PoolID
ProxyEndpointID
CreatedAt
LastUsedAt
ExpiresAt
IdleExpiresAt
RequestCount
UploadBytes
DownloadBytes
Status
RotationReason
Metadata
```

### 14.6 Client

A downstream logical client/API key identity.

Fields:

```text
ID
Name
Enabled
AuthMethod
APIKeyHash
AllowedListeners
AllowedPools
PolicySetIDs
BudgetIDs
RateLimit
IPAllowlist
CreatedAt
LastSeenAt
Metadata
```

Never store raw API keys after creation. Store a secure hash and show the token only once.

### 14.7 Policy / Rule

Policy contains ordered rules.

Rule fields:

```text
ID
PolicyID
Name
Description
Priority
Enabled
StopProcessing
Conditions
Actions
CreatedAt
UpdatedAt
```

### 14.8 RequestContext

One normalized decision context shared by protocol adapters.

Possible fields:

```text
RequestID
ConnectionID
ClientID
SessionKey
Listener
Protocol
Scheme
Method
Host
Port
Path
URL
DestinationIP
ResourceType
MIMEHint
Headers (when visible)
ContentLength
Timestamp
Tags
InspectMode
```

Fields not observable in tunnel mode must remain explicitly unknown rather than guessed.

### 14.9 RouteDecision

Suggested shape:

```text
DecisionID
Action
MatchedPolicyID
MatchedRuleIDs
PoolID
ProxyID
ChainID
CachePolicy
ThrottlePolicy
ReasonCode
ReasonText
ShadowDecisions
```

Decision reason codes must be machine-readable for debugging and analytics.

---

## 15. Proxy input normalization

ProxySieve must accept common input formats without requiring users to manually convert them.

### 15.1 Required formats

Support at least:

```text
host:port
host:port:user:pass
user:pass@host:port
http://host:port
http://user:pass@host:port
https://user:pass@host:port
socks5://host:port
socks5://user:pass@host:port
socks5h://user:pass@host:port
```

Also support multi-line text, CSV, JSON and configurable API mapping.

### 15.2 Ambiguous colon-separated formats

Because usernames/passwords may themselves contain punctuation, the parser must not silently corrupt ambiguous credentials.

Rules:

- URI format is preferred and unambiguous;
- legacy `host:port:user:pass` is supported;
- validation must reject impossible host/port shapes;
- parser should return structured parse warnings;
- UI import preview must display normalized endpoints before committing them;
- secrets remain masked in preview.

### 15.3 Provider credential templates

Support configurable username/credential templates for rotating residential gateways.

Example concept:

```yaml
credential_template:
  username: "{{account}}-country-{{country}}-os-{{os}}-session-{{session}}-lifetime-{{lifetime}}"
```

Variables can include:

```text
country
region
city
os
session
session_ttl
custom provider variables
```

Provider adapters are responsible for mapping generic variables into vendor syntax.

### 15.4 Import workflow

Import should be two-phase:

```text
parse/preview -> validate -> save
```

The preview should show:

```text
valid count
invalid count
duplicates
protocol distribution
masked host/credentials
warnings
```

### 15.5 Deduplication

Deduplicate based on a normalized endpoint identity that includes protocol/host/port and a credential identity reference without exposing secrets.

Allow users to choose:

```text
skip duplicate
update existing
create separate endpoint
```

---

## 16. Downstream gateway listeners

### 16.1 HTTP forward proxy

Required:

- standard HTTP forward requests;
- proxy authentication;
- keep-alive;
- request cancellation;
- streaming request/response bodies;
- bounded headers;
- connection/request timeouts;
- accounting hooks;
- policy hooks.

### 16.2 HTTPS CONNECT

Required:

- CONNECT target validation;
- policy evaluation before tunnel establishment;
- destination CIDR safety policy;
- upstream HTTP/SOCKS proxy tunneling;
- byte accounting in both directions;
- idle timeout;
- half-close behavior where supported;
- cancellation and cleanup;
- no assumption that tunnel bytes are HTTP after CONNECT.

Tunnel mode can reliably filter based on information observable before encryption, such as target hostname/port and client/session metadata. It must not pretend to know inner URL/resource type.

### 16.3 SOCKS5 downstream

Required:

- SOCKS5 CONNECT;
- no-auth and username/password authentication according to server configuration;
- IPv4, IPv6 and domain targets;
- policy evaluation;
- upstream direct/HTTP CONNECT/SOCKS routes;
- traffic accounting;
- safe destination validation.

SOCKS5 UDP ASSOCIATE may be added if implemented safely and tested, but lack of UDP must not block v1 if all documented v1 use cases are TCP/HTTP based. The architecture must not make later UDP support impossible.

### 16.4 Listener profiles

Allow multiple listeners, for example:

```yaml
listeners:
  - name: browser
    type: http
    bind: 127.0.0.1:8080
    policy: browser-lite

  - name: socks
    type: socks5
    bind: 127.0.0.1:1080
    policy: default
```

Each listener may specify:

```text
auth policy
default policy set
allowed clients
rate limits
destination restrictions
inspect mode policy
```

---

## 17. Upstream transport engine

### 17.1 Required upstream types

Support:

```text
DIRECT
HTTP proxy
HTTPS proxy
SOCKS5
SOCKS5 with remote DNS behavior
```

### 17.2 Connection pooling

Use protocol-appropriate connection reuse.

Requirements:

- configurable max idle connections;
- max connections per host/proxy where relevant;
- idle connection timeout;
- TLS handshake timeout;
- response header timeout;
- connect timeout;
- explicit cancellation;
- avoid sharing connections across credential/security boundaries incorrectly.

### 17.3 DNS modes

Support clearly documented choices:

```text
local DNS
upstream/proxy DNS when protocol supports it
```

DNS behavior is security-sensitive and can leak target domains. UI/config must explain the selected mode.

### 17.4 Proxy chaining

Provide chain definitions:

```yaml
chains:
  corporate-to-residential:
    hops:
      - pool: corporate
      - pool: residential-ca
```

Requirements:

- deterministic hop order;
- per-hop timeout;
- chain health status;
- chain failure reason;
- no silent fallback to direct unless explicitly configured;
- chain-level traffic/latency metrics.

If a specific combination cannot be supported correctly in v1, reject configuration with a precise error rather than silently changing semantics.

---

## 18. Proxy pool management

Pool operations:

```text
create
update
delete
enable/disable
add/remove endpoints
bulk import
assign tags
set strategy
set fallback pools
set health thresholds
set cost rules
```

Built-in selection strategies:

```text
random
round-robin
weighted-random
least-connections
least-traffic
lowest-latency
highest-health
lowest-cost
cost-aware
sticky
```

Strategy interface example:

```go
type Selector interface {
    Name() string
    Select(ctx context.Context, in SelectionContext, candidates []Candidate) (Selection, error)
}
```

SelectionContext can include:

```text
client
session
destination
required country/tags
current health
current active connections
traffic usage
configured cost
previous failures
```

Selectors must receive already-eligible candidates. Policy/eligibility filtering should not be duplicated inside every selector.

---

## 19. Session and rotation engine

### 19.1 Session strategies

Support:

```text
none
sticky by explicit session key
sticky by client
sticky by destination/domain
sticky by client + destination
```

### 19.2 Session lifetime controls

Support:

```text
absolute TTL
idle TTL
max requests
max bytes
manual rotation
rotate on failure threshold
provider-generated session lifetime
```

### 19.3 Rotation reasons

Persist reason codes:

```text
manual
expired
idle_expired
proxy_failed
health_quarantine
budget_exceeded
request_limit
byte_limit
provider_expired
policy_change
```

### 19.4 Session safety

Do not change proxy during an existing TCP tunnel. Rotation applies to subsequent eligible connections/requests.

### 19.5 Session observability

UI/API must expose:

```text
session key (masked/hashed when sensitive)
client
pool
current proxy
age
idle time
requests
bytes
expires in
status
last rotation reason
```

---

## 20. Policy and rule engine

The policy engine is the central decision system and must be deterministic, testable and explainable.

### 20.1 Conditions

Support conditions when the required data is observable:

```text
listener
client
source IP
protocol
scheme
method
host/domain
wildcard host
regex host/path
port
IP/CIDR
URL/path when visible
resource type when provided by integration or inspect mode
MIME when visible
request headers when visible
request content length
session
proxy pool
provider tags
country/time constraints
budget state
health state
```

Never evaluate an unavailable field as an accidental match. Use three-state semantics where necessary:

```text
known match
known no-match
unknown/unavailable
```

### 20.2 Actions

Built-in actions:

```text
ALLOW / continue
BLOCK
REJECT
PROXY <pool>
DIRECT
CACHE
THROTTLE
MOCK
REDIRECT
REWRITE
SET_TAG
SET_SESSION_POLICY
```

Not every action is legal in every protocol mode. For example, URL rewrite requires visibility of an HTTP request. The rules validator must reject impossible combinations.

### 20.3 Rule priority

Rules use explicit integer priority. Higher priority wins/evaluates first according to documented semantics.

Every rule supports:

```text
priority
enabled
stop_processing
conditions
actions
```

Tie behavior must be deterministic; use stable secondary ordering such as rule ID/order field.

### 20.4 Policy composition

A client/listener can have a policy chain:

```text
system security policy
organization/default policy
listener policy
client policy
runtime override
```

Security policy cannot be bypassed by a lower-priority user policy unless the user is explicitly authorized and the setting permits it.

### 20.5 Decision trace

Every debug/shadow decision can produce:

```text
rules evaluated
conditions evaluated
unknown fields
matched rules
actions selected
final route
reason code
elapsed evaluation time
```

Normal production mode should avoid expensive full tracing unless requested/sampled.

### 20.6 Rule simulator

API/UI input:

```text
URL or host/port
method
client
listener
session
resource type
headers (optional)
mode/tunnel visibility
```

Output:

```text
final decision
matched rule
evaluation trace
selected pool
candidate count
warnings about unavailable attributes
```

---

## 21. Browser-aware optimization layer

Protocol-level filtering alone cannot see an encrypted browser request path/resource type inside a CONNECT tunnel. Browser integrations solve this before traffic enters the paid proxy path.

### 21.1 Shared integration protocol

Define a lightweight local/control API contract so browser SDKs can:

```text
fetch active browser policy
classify a request
tell ProxySieve that a request was locally blocked
attach client/session metadata
report estimated size information when available
```

Avoid forcing a network round-trip to the gateway for every browser request if policies can be safely cached in the SDK. Use versioned policy snapshots and refresh/invalidation.

### 21.2 Playwright integration

Package:

```text
integrations/playwright
```

Support:

- attach to BrowserContext;
- optional Page-level attach;
- classify by URL/resource type;
- abort blocked resource types before network transfer;
- preserve navigation-critical requests;
- attach ProxySieve client/session identity;
- report local block counters;
- configurable presets;
- cleanup/unregister routing hooks.

Preset examples:

```text
passthrough
browser-lite
browser-aggressive
api-only
bandwidth-saver
privacy-safe
```

### 21.3 Puppeteer integration

Package:

```text
integrations/puppeteer
```

Support equivalent behavior via request interception, including safe coexistence rules/documentation when a user already uses interception.

### 21.4 Selenium foundation

Document at least two paths:

```text
standard proxy-only integration
CDP/BiDi-aware optional integration where supported
```

Do not build browser-version-specific hacks into Go core.

### 21.5 Generic applications

Document examples for:

```text
curl
wget
Python requests
httpx
aiohttp
Scrapy
Node fetch/undici/axios
Go net/http
Java clients
.NET HttpClient
Selenium
Chrome/Chromium proxy flags
```

The generic client path must work without SDK, with less granular filtering.

---

## 22. Traffic optimization engine

Optimization is policy-driven, not magic.

### 22.1 Common optimizations

Support policy/presets for:

```text
image blocking
font blocking
media/video blocking
analytics/tracker domains
ad domains
prefetch/preload requests when browser integration exposes them
telemetry endpoints
known unnecessary third-party assets
```

### 22.2 Direct routing

`DIRECT` bypasses paid proxy but may expose the host's real public IP and create geographic/session inconsistency.

Therefore:

- DIRECT is disabled in safe default profile;
- enabling it shows a clear UI warning;
- policies can restrict DIRECT to explicit allowlisted domains;
- no automatic “smart direct” based solely on file extension;
- direct traffic is measured separately.

### 22.3 Header/cookie reduction

Only perform header/cookie rewriting in modes where HTTP contents are visible and the user explicitly enables it. Never remove auth/session data based on heuristic assumptions.

### 22.4 Response limits

Allow optional limits:

```text
max response bytes
max streaming duration
max bandwidth rate
```

Behavior must be explicit: abort, truncate only where protocol semantics allow, or reject before transfer when size is known.

---

## 23. Health and proxy quality engine

### 23.1 Metrics

Track rolling measurements:

```text
DNS/connect success
proxy auth failure
connect latency
TLS handshake failure
time to first byte
request success rate
timeout rate
HTTP 403
HTTP 407
HTTP 429
HTTP 5xx
throughput
recent consecutive failures
last success
last failure
```

HTTP status interpretation must be configurable. A `403` may reflect the target rather than a broken proxy.

### 23.2 Health states

Use:

```text
UNKNOWN
HEALTHY
DEGRADED
QUARANTINED
HALF_OPEN
DISABLED
```

### 23.3 Score

Expose a 0–100 quality score, but keep component metrics visible. Score weighting must be configurable and documented.

Do not make selection dependent on an opaque score that users cannot explain.

### 23.4 Active health checks

Support optional active checks with configurable targets and intervals.

Requirements:

- do not hammer expensive residential proxies by default;
- allow passive-only health mode;
- health check traffic must be accounted separately;
- endpoint URLs must be configurable;
- rate limit health checks globally and per pool.

---

## 24. Circuit breaker, retries and failover

### 24.1 Circuit breaker

States:

```text
closed
open
half-open
```

Driven by configurable rolling failure thresholds.

### 24.2 Retry rules

Retry only when safe.

Consider:

```text
method/idempotency
whether request body is replayable
whether any response bytes were delivered
failure type
policy retry budget
session affinity
```

Default:

- conservative retries for idempotent requests;
- no blind retry for POST/checkout-like operations;
- exponential backoff with jitter;
- small retry count;
- per-request retry budget;
- total traffic cost considered.

### 24.3 Failover

Failover may select:

```text
another endpoint in same pool
fallback pool
alternative chain
reject
```

Never silently fall back to DIRECT unless policy explicitly allows IP exposure.

---

## 25. Traffic accounting

Accounting must be designed into the network path, not added as dashboard estimates later.

### 25.1 Required actual measurements

Measure:

```text
client -> ProxySieve upload bytes
ProxySieve -> client download bytes
ProxySieve -> upstream upload bytes
upstream -> ProxySieve download bytes
direct-route bytes
cache-served bytes
health-check bytes
inspect overhead where meaningful
```

### 25.2 Dimensions

Aggregate by:

```text
time bucket
client
listener
session
host/domain when observable
proxy endpoint
pool
provider
chain
policy
rule/action
protocol
```

Avoid unbounded label cardinality in Prometheus. High-cardinality dimensions belong in SQLite/analytics tables, not raw metrics labels.

### 25.3 Actual vs estimated savings

Expose separate metrics:

```text
actual_upstream_bytes
blocked_request_count
cache_served_bytes
direct_bytes
estimated_avoided_bytes
```

`estimated_avoided_bytes` must always be labeled as an estimate.

Estimation may use:

```text
historical same-resource size
historical same-host/resource-type average
shadow run baseline
configured estimate model
```

Never fabricate savings when no evidence exists.

---

## 26. Cost accounting and budgets

### 26.1 Proxy cost model

Allow configurable prices:

```text
per GB upload+download
per GB download only
flat informational monthly cost
custom provider quota metadata
```

Keep cost calculation deterministic and visible.

### 26.2 Budget scopes

Support budgets by:

```text
system
client
proxy
pool
provider
session
domain/policy where practical
daily
weekly
monthly
rolling window
```

### 26.3 Budget thresholds

Support:

```text
soft warning threshold
hard threshold
```

Actions:

```text
alert
reject
switch pool
apply fallback policy
throttle
```

DIRECT must still require explicit permission.

### 26.4 Projection

Dashboard may calculate projected daily/monthly usage using recent rate, clearly labeled as projection.

---

## 27. Cache subsystem

### 27.1 Cache types

Implement:

```text
DNS cache
policy/rule snapshot cache
health state cache
HTTP response cache
```

### 27.2 HTTP cache safety

Default response cache should be OFF unless a preset enables safe static caching.

Only cache eligible requests, normally:

```text
GET/HEAD
no Authorization unless explicitly allowed
no private/no-store directives
bounded response size
explicit TTL policy
```

Respect relevant HTTP cache semantics where practical.

### 27.3 Storage

Provide:

```text
memory cache
disk cache
```

Interfaces must permit external cache adapters later.

### 27.4 Cache observability

Track:

```text
hit
miss
bypass
expired
evicted
bytes served
bytes stored
hit ratio
```

---

## 28. Shadow policy engine

Shadow mode lets users evaluate policy changes without affecting production traffic.

### 28.1 Behavior

For each sampled/eligible request:

```text
active policy -> actual decision
shadow policy -> simulated decision only
```

### 28.2 Output

Expose comparisons:

```text
decision distribution
proxy request count
block count
direct count
pool selection differences
estimated upstream bytes
policy evaluation latency
```

### 28.3 Safety

Shadow policies must never create an upstream connection solely to determine what would have happened unless the user explicitly runs a benchmark experiment.

---

## 29. Optional HTTPS Inspect Mode

Inspect mode is included in v1 as an **advanced optional module**, OFF by default.

### 29.1 Goals

When explicitly enabled for selected hosts, allow visibility of:

```text
HTTP method
full URL/path
request headers
response headers
content type
request/response sizes
```

Optional body inspection must have strict size limits and privacy controls.

### 29.2 CA management

Provide:

```text
generate local CA
show certificate fingerprint
export CA certificate
rotate CA
certificate cache
host certificate generation
```

Private keys require restrictive filesystem permissions and must never appear in logs/exports by default.

### 29.3 Include/exclude scopes

Configuration example:

```yaml
inspect:
  enabled: false
  include:
    - "*.example.com"
  exclude:
    - "*.bank.example"
```

### 29.4 Failure behavior

If interception fails because of pinning or unsupported TLS behavior, follow explicit configured behavior:

```text
fail closed
fall back to tunnel for allowlisted domains
reject
```

Do not silently downgrade security.

### 29.5 Module boundary

Whether implemented using an existing Go proxy library or custom components, external library-specific types must remain inside an adapter package. Public ProxySieve APIs must use ProxySieve domain types.

---

## 30. Event and alert subsystem

### 30.1 Domain events

Examples:

```text
proxy.added
proxy.updated
proxy.failed
proxy.quarantined
proxy.recovered
pool.degraded
request.blocked
request.failed
session.created
session.rotated
budget.warning
budget.exceeded
cache.hit
rule.matched
provider.refresh_failed
system.config_reloaded
security.auth_failed
```

### 30.2 Event bus

Default: in-process bounded event bus.

Requirements:

- subscribers cannot block network hot paths;
- bounded buffers/backpressure policy;
- dropped-event metric;
- persistent audit/security events where required;
- external event transport can be added later.

### 30.3 Alerts

Built-in destinations:

```text
UI notification
generic webhook
```

Alert rules include:

```text
pool health threshold
budget threshold
high error rate
proxy source refresh failure
cache pressure
storage pressure
security events
```

Webhook delivery needs timeout, retry cap, signature secret support and delivery logs.

---

## 31. Extension architecture

### 31.1 Built-in extensions

Go interfaces compiled into the binary for:

```text
provider adapters
proxy selectors
storage adapters
cache adapters
policy actions
alert sinks
```

### 31.2 External extensions

Do not base community plugins on Go's native `plugin` package.

Use a versioned external process protocol, for example:

```text
ProxySieve <-> extension process
```

Transport may be local RPC/stdio/Unix socket/named pipe as chosen during implementation.

Requirements:

- version handshake;
- capability negotiation;
- timeout/cancellation;
- process lifecycle management;
- crash isolation;
- structured errors;
- optional sandbox guidance;
- no extension access to raw secrets unless its declared capability requires them.

### 31.3 Extension API version

Start with:

```text
Extension API v1
```

Manifest example:

```yaml
name: custom-provider
version: 0.1.0
api_version: 1
capabilities:
  - proxy-provider
```

### 31.4 Example extensions

Ship at least:

```text
example provider
example selector
example webhook/alert sink
```

Each example should be intentionally small and documented for fork authors.

---

# Part V — Control Plane, Security, Persistence & UX

## 32. Configuration system

Configuration is a public product interface and must be treated as versioned API.

### 32.1 Configuration sources and precedence

Define deterministic precedence:

```text
built-in defaults
    ↓
config file
    ↓
environment variables
    ↓
CLI flags
    ↓
runtime/API overrides
```

Runtime overrides must be clearly visible in the UI/API so users can understand the effective value.

### 32.2 Config file

Default filename:

```text
config.yaml
```

Every config file has a schema version:

```yaml
version: 1
```

Provide:

```bash
proxysieve config validate
proxysieve config print-effective
proxysieve config migrate
```

### 32.3 Environment variable naming

Use prefix:

```text
PROXYSIEVE_
```

Examples:

```text
PROXYSIEVE_LOG_LEVEL
PROXYSIEVE_DATA_DIR
PROXYSIEVE_API_BIND
PROXYSIEVE_ADMIN_BIND
```

Secrets should be passable via environment or secret files instead of being required inline in YAML.

### 32.4 Hot reload

Support safe hot reload for:

```text
rules/policies
proxy pools
proxy endpoints
provider source settings where possible
budgets
health thresholds
alerts
logging level
```

Listener bind changes and other structural changes may require restart; the API must explicitly state whether a change is hot-applied or pending restart.

### 32.5 Example full config shape

```yaml
version: 1

server:
  data_dir: "~/.proxysieve"
  shutdown_timeout: 20s

listeners:
  - name: http
    type: http
    bind: "127.0.0.1:8080"
    auth: local
    policy: default

  - name: socks
    type: socks5
    bind: "127.0.0.1:1080"
    auth: local
    policy: default

admin:
  enabled: true
  bind: "127.0.0.1:9090"
  tls: false

storage:
  driver: sqlite
  path: "~/.proxysieve/proxysieve.db"

traffic:
  retention_days: 30
  aggregation_interval: 1m

inspect:
  enabled: false

cache:
  response:
    enabled: false
  dns:
    enabled: true

security:
  deny_private_networks_for_untrusted_clients: true

logging:
  level: info
  format: json
  capture_headers: false
  capture_bodies: false
```

Actual schema may evolve during implementation, but semantic responsibilities must remain equivalent.

---

## 33. Secrets and credential management

### 33.1 Secret model

Secrets include:

```text
upstream proxy passwords
provider API keys
client API keys
admin session secrets
webhook signing secrets
HTTPS inspection CA private keys
```

### 33.2 Storage

Implement a `SecretStore` abstraction.

Default local mode should support encrypted-at-rest secret material using a locally generated master key or OS-supported secure storage where practical.

Do not invent weak encryption. Use well-reviewed cryptographic primitives from the Go standard/x crypto ecosystem.

### 33.3 Redaction

All logging/error serialization must pass through redaction helpers.

Always redact:

```text
Authorization
Proxy-Authorization
Cookie
Set-Cookie
X-API-Key
known token fields
provider secrets
URL userinfo
```

Proxy display:

```text
socks5://user:********@proxy.example:1080
```

### 33.4 Export behavior

Config/export/backup must omit raw secrets by default.

Optional encrypted secret export requires an explicit flag and clear warning.

---

## 34. Security architecture

ProxySieve is network infrastructure. Security cannot be postponed to a later version.

### 34.1 Local-only default

Default listener/admin binds:

```text
127.0.0.1
::1 where appropriate
```

Binding to `0.0.0.0` or public interfaces should display a warning and require authentication unless explicitly running an insecure dev mode.

### 34.2 Downstream authentication

Support:

```text
local/trusted mode
username/password proxy auth
API key/token identity where compatible
IP allowlist as an additional control
```

Authentication and logical client identity should be separable so different auth methods can map to the same client record.

### 34.3 Admin authentication

Single-user first-run flow:

- generate admin account/setup token;
- require password setup for non-loopback access;
- secure password hashing using an appropriate password hashing algorithm;
- secure session cookies;
- CSRF protection for cookie-authenticated mutation endpoints;
- login rate limiting;
- audit successful/failed admin auth events.

### 34.4 RBAC

Built-in roles:

```text
admin
operator
viewer
```

Suggested permissions:

**admin**

```text
all configuration
users/API keys
security
secrets
providers
rules
proxy pools
system operations
backup/restore
```

**operator**

```text
proxies
pools
rules/policies
sessions
health
traffic operations
alerts
```

**viewer**

```text
read dashboards
analytics
health
sanitized logs
```

Define permissions as capabilities internally so future custom roles remain possible.

### 34.5 SSRF/confused-deputy protection

Default policy for untrusted downstream clients must deny access to sensitive/private destinations unless explicitly allowed.

Protect at minimum:

```text
loopback
link-local
cloud metadata ranges/endpoints
private RFC1918 networks
IPv6 unique-local/link-local
Unix/local-only services where applicable
```

Resolve DNS safely and consider rebinding. Validation cannot rely solely on hostname string checks.

Allow trusted local users to override this policy explicitly for legitimate internal proxy use.

### 34.6 Rate limits and resource limits

Implement limits for:

```text
connections per client
requests per second where meaningful
concurrent tunnels
header sizes
request metadata size
admin/API requests
webhook queue
log queue
event queue
cache size
```

All queues must be bounded or have an explicit backpressure strategy.

### 34.7 Security headers/CORS

Admin API/UI:

- restrictive CORS default;
- secure headers;
- CSP appropriate to bundled dashboard;
- no wildcard credentialed origins;
- no secrets in frontend localStorage where avoidable.

### 34.8 TLS for admin API

Support optional HTTPS for remote admin deployments with user-provided certificate/key. Document reverse-proxy deployment as an alternative.

### 34.9 Security disclosure

`SECURITY.md` must include:

```text
supported versions
private vulnerability reporting process
what information to include
request not to publish credentials
```

---

## 35. Persistence and database schema

Use migrations in a dedicated directory, for example:

```text
internal/storage/sqlite/migrations/
  000001_initial.sql
  000002_traffic_aggregates.sql
  ...
```

Do not rely on ORM auto-migration in production.

### 35.1 Core tables

Expected logical tables:

```text
settings
users
roles / permissions if needed
clients
api_keys
proxy_sources
proxy_providers
proxy_endpoints
proxy_endpoint_tags
proxy_pools
proxy_pool_members
proxy_chains
proxy_chain_hops
sessions
policies
rules
budgets
budget_usage
health_snapshots
proxy_state
traffic_events or sampled request records
traffic_aggregates_minute
traffic_aggregates_hour
traffic_aggregates_day
cache_metadata
alerts
alert_deliveries
audit_log
webhooks
schema_migrations
```

Exact normalization may change after profiling, but responsibilities must remain represented.

### 35.2 Traffic retention strategy

Raw request events can become enormous. Default design:

```text
short-lived detailed records
    ↓ aggregation
minute buckets
    ↓ aggregation/retention
hour/day buckets
```

Provide retention settings.

Example:

```text
detailed events: 24–72 hours
minute aggregates: 7 days
hour aggregates: 30–90 days
daily aggregates: long term
```

Defaults should be conservative on disk usage and configurable.

### 35.3 Index strategy

Indexes should cover common filters:

```text
timestamp
client_id
proxy_id
pool_id
session_id
host/domain hash or normalized domain
status/action
```

Measure write amplification before adding every possible composite index.

### 35.4 Database maintenance

CLI/API:

```bash
proxysieve db status
proxysieve db compact
proxysieve db migrate
```

Background retention/aggregation jobs must be cancellable and must not block gateway traffic.

---

## 36. REST API specification

Prefix all endpoints:

```text
/api/v1
```

### 36.1 System

```text
GET  /api/v1/system/info
GET  /api/v1/system/health
GET  /api/v1/system/ready
GET  /api/v1/system/config/effective
POST /api/v1/system/reload
```

### 36.2 Authentication/users

```text
POST /api/v1/auth/login
POST /api/v1/auth/logout
GET  /api/v1/auth/me
GET  /api/v1/users
POST /api/v1/users
PATCH /api/v1/users/{id}
DELETE /api/v1/users/{id}
```

### 36.3 Clients/API keys

```text
GET    /api/v1/clients
POST   /api/v1/clients
GET    /api/v1/clients/{id}
PATCH  /api/v1/clients/{id}
DELETE /api/v1/clients/{id}
POST   /api/v1/clients/{id}/api-keys
DELETE /api/v1/clients/{id}/api-keys/{keyId}
```

API key creation response shows raw token only once.

### 36.4 Proxies

```text
GET    /api/v1/proxies
POST   /api/v1/proxies
POST   /api/v1/proxies/import/preview
POST   /api/v1/proxies/import
GET    /api/v1/proxies/{id}
PATCH  /api/v1/proxies/{id}
DELETE /api/v1/proxies/{id}
POST   /api/v1/proxies/{id}/test
POST   /api/v1/proxies/{id}/enable
POST   /api/v1/proxies/{id}/disable
```

### 36.5 Sources/providers

```text
GET    /api/v1/providers
GET    /api/v1/providers/capabilities
GET    /api/v1/sources
POST   /api/v1/sources
PATCH  /api/v1/sources/{id}
DELETE /api/v1/sources/{id}
POST   /api/v1/sources/{id}/refresh
POST   /api/v1/sources/{id}/validate
```

### 36.6 Pools

```text
GET    /api/v1/pools
POST   /api/v1/pools
GET    /api/v1/pools/{id}
PATCH  /api/v1/pools/{id}
DELETE /api/v1/pools/{id}
POST   /api/v1/pools/{id}/members
DELETE /api/v1/pools/{id}/members/{proxyId}
GET    /api/v1/pools/{id}/health
```

### 36.7 Chains

```text
GET    /api/v1/chains
POST   /api/v1/chains
PATCH  /api/v1/chains/{id}
DELETE /api/v1/chains/{id}
POST   /api/v1/chains/{id}/test
```

### 36.8 Sessions

```text
GET    /api/v1/sessions
GET    /api/v1/sessions/{id}
POST   /api/v1/sessions/{id}/rotate
DELETE /api/v1/sessions/{id}
```

### 36.9 Policies/rules

```text
GET    /api/v1/policies
POST   /api/v1/policies
GET    /api/v1/policies/{id}
PATCH  /api/v1/policies/{id}
DELETE /api/v1/policies/{id}
POST   /api/v1/policies/{id}/clone
POST   /api/v1/policies/{id}/validate
POST   /api/v1/policies/simulate
GET    /api/v1/presets
POST   /api/v1/presets/{id}/apply
```

Rules may be nested resources or managed through the policy document. Choose one consistent model and document it.

### 36.10 Shadow policies

```text
GET    /api/v1/shadow
POST   /api/v1/shadow
PATCH  /api/v1/shadow/{id}
DELETE /api/v1/shadow/{id}
GET    /api/v1/shadow/{id}/comparison
```

### 36.11 Traffic/analytics

```text
GET /api/v1/traffic/summary
GET /api/v1/traffic/timeseries
GET /api/v1/traffic/domains
GET /api/v1/traffic/clients
GET /api/v1/traffic/proxies
GET /api/v1/traffic/pools
GET /api/v1/traffic/policies
GET /api/v1/traffic/live
```

Query parameters must support bounded date ranges, pagination and filters.

### 36.12 Health

```text
GET  /api/v1/health/proxies
GET  /api/v1/health/pools
POST /api/v1/health/proxies/{id}/check
POST /api/v1/health/pools/{id}/check
```

### 36.13 Budgets

```text
GET    /api/v1/budgets
POST   /api/v1/budgets
PATCH  /api/v1/budgets/{id}
DELETE /api/v1/budgets/{id}
GET    /api/v1/budgets/{id}/usage
```

### 36.14 Cache

```text
GET  /api/v1/cache/stats
POST /api/v1/cache/purge
POST /api/v1/cache/purge/domain
```

### 36.15 Alerts/webhooks

```text
GET    /api/v1/alerts
POST   /api/v1/alerts
PATCH  /api/v1/alerts/{id}
DELETE /api/v1/alerts/{id}
GET    /api/v1/webhooks
POST   /api/v1/webhooks
PATCH  /api/v1/webhooks/{id}
DELETE /api/v1/webhooks/{id}
POST   /api/v1/webhooks/{id}/test
```

### 36.16 Audit/logs/events

```text
GET /api/v1/audit
GET /api/v1/logs
GET /api/v1/events
GET /api/v1/events/stream
```

### 36.17 Extensions

```text
GET  /api/v1/extensions
GET  /api/v1/extensions/{id}
POST /api/v1/extensions/{id}/enable
POST /api/v1/extensions/{id}/disable
```

### 36.18 Import/export/backup

```text
POST /api/v1/export
POST /api/v1/import/preview
POST /api/v1/import
POST /api/v1/backup
POST /api/v1/restore/validate
POST /api/v1/restore
```

Restore requires elevated admin permission and explicit confirmation.

### 36.19 API response conventions

Use consistent error envelope:

```json
{
  "error": {
    "code": "PROXY_AUTH_FAILED",
    "message": "Upstream proxy authentication failed",
    "request_id": "...",
    "details": {}
  }
}
```

Rules:

- machine-readable error codes;
- human-readable message;
- request/correlation ID;
- no secrets in details;
- appropriate HTTP status;
- pagination metadata for lists.

---

## 37. OpenAPI and SDK generation

`openapi.yaml` or generated canonical spec must be committed.

Requirements:

- CI verifies generated clients are up to date;
- TypeScript admin API client should be generated or strongly derived from the schema;
- Go SDK can be handwritten/generated later in the milestone but must use stable public models;
- breaking API changes require changelog and migration notes.

---

## 38. CLI design

Binary:

```text
proxysieve
```

### 38.1 Core commands

```text
proxysieve start
proxysieve status
proxysieve version
proxysieve doctor
```

### 38.2 Config

```text
proxysieve config validate
proxysieve config print-effective
proxysieve config migrate
```

### 38.3 Proxies

```text
proxysieve proxy list
proxysieve proxy add <proxy>
proxysieve proxy import <file>
proxysieve proxy test <id|all>
proxysieve proxy enable <id>
proxysieve proxy disable <id>
proxysieve proxy remove <id>
```

### 38.4 Pools

```text
proxysieve pool list
proxysieve pool create
proxysieve pool show <id>
proxysieve pool test <id>
```

### 38.5 Policies

```text
proxysieve policy list
proxysieve policy show
proxysieve policy validate
proxysieve policy simulate
proxysieve policy apply-preset
```

### 38.6 Sessions

```text
proxysieve session list
proxysieve session show
proxysieve session rotate
proxysieve session delete
```

### 38.7 Traffic/health

```text
proxysieve traffic summary
proxysieve traffic top domains
proxysieve traffic top proxies
proxysieve health
```

### 38.8 Data operations

```text
proxysieve export
proxysieve import
proxysieve backup
proxysieve restore
proxysieve db status
proxysieve db compact
```

### 38.9 Developer commands

```text
proxysieve dev
proxysieve debug rule
proxysieve debug connection
```

### 38.10 Doctor

`proxysieve doctor` should check:

```text
config validity
data directory permissions
database/migrations
listener availability
admin port
DNS
outbound direct connectivity when enabled
proxy pool status
provider source errors
CA status when inspect enabled
storage pressure
version/update information when explicitly allowed
```

Exit non-zero when critical problems are found.

---

## 39. Admin dashboard information architecture

The dashboard is first-class, not a decorative afterthought.

### 39.1 Navigation

Recommended sidebar:

```text
Dashboard
Live Traffic
Analytics

Proxy Pools
Proxies
Providers & Sources
Sessions

Policies
Rules
Presets
Rule Simulator
Shadow Policies

Budgets
Cache

Clients
API Keys

Health
Events
Alerts
Logs
Audit Log

Extensions

Settings
System
```

### 39.2 Dashboard overview

Show at minimum:

```text
upstream proxy traffic today
client traffic today
direct traffic
cache-served traffic
blocked request count
estimated avoided bytes with estimate badge
configured proxy cost today
active sessions
healthy/degraded/quarantined proxies
request success rate
P50/P95 latency
```

Charts:

```text
traffic over time
actions over time
cost over time
health distribution
```

### 39.3 Live Traffic

Table columns when available:

```text
time
method/action
host
path (only when visible)
client
session
pool
proxy
status
upload
download
latency
matched rule
```

Features:

```text
pause/resume
search
filter
column chooser
detail drawer
sampled/full mode indicator
export current view
```

Never show sensitive headers/bodies by default.

### 39.4 Proxy management

Proxy detail page:

```text
masked endpoint
protocol
provider/source
pools/tags
country metadata
health score
latency
success/error rates
active sessions
traffic/cost
last failures
manual test
quarantine/enable/disable
```

### 39.5 Policy editor

Provide both:

```text
form/visual rule builder
YAML/JSON advanced editor
```

Both edit the same canonical policy model.

Capabilities:

```text
validation
priority reorder
condition groups
warnings for unavailable tunnel-mode fields
rule simulator
clone rule
import/export
```

### 39.6 Budget UI

Show:

```text
used / limit
percentage
projection
thresholds
action on exceed
history
```

### 39.7 Health UI

Views:

```text
all proxies
pool health
failure reasons
latency distribution
quarantine history
manual health test
```

### 39.8 Security/admin UI

Settings should make dangerous features visually explicit:

```text
public bind
DIRECT routing
HTTPS inspect
private network access
body capture
secret export
```

Use confirmations for destructive/security-sensitive actions.

---

## 40. Presets

Ship useful built-in policy packs.

### 40.1 passthrough

```text
proxy everything through selected pool
no content optimization
```

### 40.2 browser-lite

Browser adapter blocks commonly unnecessary heavy assets conservatively:

```text
media
optional fonts
known tracking domains
```

Images should be configurable rather than assumed unnecessary for all browser workloads.

### 40.3 browser-aggressive

```text
images
media
fonts
trackers
ads
prefetch where safely identified
```

Must warn that page behavior/visual tests may break.

### 40.4 api-only

Allow document/XHR/fetch-like API workloads and block browser asset traffic when integration metadata exists.

### 40.5 bandwidth-saver

Prioritize minimizing paid upstream bytes; may combine cache and strict browser filters.

### 40.6 privacy-safe

No DIRECT route, no body capture, no inspect mode, strict tracker policies.

### 40.7 Preset invariants

Presets are templates, not hidden magic. Users can inspect every generated rule before applying.

---

## 41. Import, export, backup and restore

### 41.1 Export

Support exporting:

```text
config
policies
presets
proxy metadata
pools
provider/source definitions
budgets
alerts
```

Default excludes secrets.

### 41.2 Portable export format

Use a versioned archive/manifest, for example:

```text
manifest.json
config.yaml
policies/
pools.json
sources.json
```

### 41.3 Backup

Local backup should include database/config and optional encrypted secrets.

Example:

```bash
proxysieve backup --output proxysieve-backup.zip
```

### 41.4 Restore

Flow:

```text
validate archive
show compatibility/migration report
create safety backup
apply restore
run migrations
verify
```

Do not overwrite a live database without safety checks.

---

## 42. Logging, tracing and observability

### 42.1 Structured logs

Default production format:

```text
JSON
```

Developer mode may use pretty console logs.

Common fields:

```text
timestamp
level
component
request_id
connection_id
client_id
session_id
proxy_id
pool_id
event/error code
```

### 42.2 Log levels

```text
trace
debug
info
warn
error
```

No per-byte logging.

### 42.3 Prometheus-compatible metrics

Expose:

```text
/metrics
```

Metric families should include:

```text
connections
requests/tunnels
bytes
latency histograms
proxy health states
circuit breaker state
cache
retry/failover
policy decisions
budget thresholds
event queue drops
provider refresh status
```

Avoid labels containing raw URL/session IDs.

### 42.4 OpenTelemetry

Provide optional tracing/metrics hooks.

Do not require an OTel collector for normal operation.

### 42.5 Health endpoints

```text
/health  -> process/live health
/ready   -> ready to accept intended traffic
```

Readiness should consider critical dependencies such as database migration state and required listeners.

---

# Part VI — Repository Structure & Engineering Standards

## 43. Canonical repository structure

Target structure:

```text
proxysieve/
├── cmd/
│   └── proxysieve/
│       └── main.go
│
├── pkg/
│   ├── gateway/
│   ├── proxy/
│   ├── policy/
│   ├── routing/
│   ├── session/
│   ├── health/
│   ├── traffic/
│   ├── budget/
│   ├── cache/
│   ├── provider/
│   ├── extension/
│   ├── events/
│   ├── auth/
│   ├── config/
│   └── sdk/
│
├── internal/
│   ├── app/
│   ├── api/
│   ├── admin/
│   ├── storage/
│   │   ├── memory/
│   │   └── sqlite/
│   │       └── migrations/
│   ├── transport/
│   │   ├── httpforward/
│   │   ├── connect/
│   │   └── socks5/
│   ├── upstream/
│   ├── inspect/
│   ├── security/
│   ├── metrics/
│   ├── scheduler/
│   └── buildinfo/
│
├── web/
│   ├── src/
│   ├── public/
│   ├── tests/
│   ├── package.json
│   ├── pnpm-lock.yaml
│   └── vite.config.ts
│
├── integrations/
│   ├── playwright/
│   ├── puppeteer/
│   └── selenium/
│
├── extensions/
│   ├── examples/
│   │   ├── provider/
│   │   ├── selector/
│   │   └── alert-sink/
│   └── protocol/
│
├── examples/
│   ├── curl/
│   ├── playwright/
│   ├── puppeteer/
│   ├── selenium/
│   ├── python-requests/
│   ├── python-httpx/
│   ├── node-fetch/
│   └── go-http/
│
├── configs/
│   ├── config.example.yaml
│   └── presets/
│
├── api/
│   └── openapi.yaml
│
├── docs/
│   ├── architecture.md
│   ├── security.md
│   ├── proxy-formats.md
│   ├── policies.md
│   ├── browser-integrations.md
│   ├── inspect-mode.md
│   ├── extensions.md
│   ├── deployment.md
│   ├── troubleshooting.md
│   └── development.md
│
├── benchmarks/
│   ├── scenarios/
│   ├── results/
│   └── README.md
│
├── test/
│   ├── integration/
│   ├── protocol/
│   ├── e2e/
│   ├── fixtures/
│   └── testserver/
│
├── tools/
├── scripts/
│
├── .github/
│   ├── workflows/
│   │   ├── ci.yml
│   │   ├── frontend.yml
│   │   ├── integration.yml
│   │   ├── codeql.yml            # only if advanced setup is selected
│   │   ├── release.yml
│   │   └── benchmark.yml
│   ├── ISSUE_TEMPLATE/
│   ├── dependabot.yml
│   └── pull_request_template.md
│
├── README.md
├── PLAN.md
├── CHANGELOG.md
├── CONTRIBUTING.md
├── SECURITY.md
├── SUPPORT.md
├── CODE_OF_CONDUCT.md
├── CODEOWNERS
├── LICENSE
├── NOTICE
├── Makefile
├── Dockerfile
├── docker-compose.yml
├── .goreleaser.yml
├── .golangci.yml
├── .editorconfig
├── .gitattributes
├── .gitignore
├── go.mod
└── go.sum
```

This is a target structure, not permission to create empty placeholder directories unnecessarily. Create modules when the milestone reaches them.

---

## 44. Public vs internal Go packages

Use `pkg/` only for contracts and code that external forks/extensions can reasonably depend on.

Good candidates:

```text
proxy domain types
policy model/evaluator contracts
selector contracts
provider contracts
traffic models
config public models
extension protocol types
```

Use `internal/` for application wiring and implementations that should not become compatibility promises:

```text
SQLite implementation
HTTP handler wiring
specific gateway implementation details
inspect backend
scheduler
admin session implementation
build embedding
```

Do not expose third-party library types from public interfaces.

---

## 45. Dependency direction

Preferred dependency direction:

```text
public domain/contracts
        ↑
application services
        ↑
adapters (HTTP/SOCKS/SQLite/provider implementations)
        ↑
composition root in internal/app + cmd
```

Forbidden examples:

```text
pkg/policy -> React/UI
pkg/traffic -> SQLite concrete package
pkg/proxy -> specific provider brand
pkg/gateway -> admin HTTP handler
```

Use architecture tests/static review to prevent accidental dependency inversion violations.

---

## 46. Go coding standards

Requirements:

- `gofmt` clean;
- `go vet` clean;
- linter configuration committed;
- package comments for public packages;
- comments for exported public API where useful;
- context as first parameter for cancellable operations;
- sentinel/typed errors for machine decisions, wrapped with context;
- no panic for normal network/user errors;
- panic recovery at process boundaries only;
- graceful shutdown;
- no data races;
- no goroutine leaks;
- no unbounded channel/queue growth;
- no global mutable singletons for core services;
- explicit clocks/randomness interfaces where deterministic tests need them;
- secrets represented with types/helpers that discourage accidental string logging.

Prefer simple readable Go over clever abstractions.

---

## 47. TypeScript/frontend standards

Requirements:

- strict TypeScript mode;
- ESLint/lint equivalent configured;
- formatting tool configured;
- no `any` except justified boundary code;
- reusable API types generated/derived from OpenAPI;
- data fetching isolated from presentation;
- error/loading/empty states for every data page;
- no secret tokens written to console;
- accessibility labels for forms/actions;
- test important reducers/hooks/components and key flows;
- avoid giant global state store when server state can be managed as server state.

---

## 48. Commit and PR conventions

Use Conventional Commits:

```text
feat:
fix:
perf:
refactor:
docs:
test:
build:
ci:
chore:
```

Examples:

```text
feat(policy): add deterministic domain matcher
fix(socks5): close upstream on client cancellation
perf(traffic): batch minute aggregates
```

PR title should follow the same style because squash merge makes PR title the final main-branch commit.

PR template checklist should include:

```text
what changed
why
security impact
traffic/accounting impact
backward compatibility
tests added
benchmark impact if hot path
docs updated
```

---

# Part VII — Test Strategy & Quality Gates

## 49. Unit tests

Unit-test at minimum:

```text
proxy parser/normalizer
rule condition matching
rule priority and composition
selector strategies
session expiration/rotation
health scoring
circuit breaker transitions
retry eligibility
budget calculations
cost calculations
traffic aggregation
config precedence
config validation
redaction
CIDR destination safety
cache eligibility
extension protocol serialization/version checks
```

Use table-driven tests where natural.

---

## 50. Fuzz tests

High-value fuzz targets:

```text
proxy URI/input parser
HTTP CONNECT target parser
SOCKS address parsing
policy JSON/YAML decode
header redaction
config import
archive restore validation
extension protocol decode
```

A malformed input must fail cleanly, never panic or allocate unbounded memory.

---

## 51. Race tests

Run:

```bash
go test -race ./...
```

At least in CI on Linux.

Pay special attention to:

```text
session map
proxy health state
round-robin counters
traffic counters
event bus
cache
hot reload policy snapshots
graceful shutdown
```

---

## 52. Protocol integration matrix

Automated matrix must cover where technically supported:

| Downstream | Route | Upstream | Required |
|---|---|---|---|
| HTTP | proxy | HTTP | yes |
| HTTP | proxy | SOCKS5 | yes |
| HTTP | direct | direct | yes |
| CONNECT | proxy | HTTP CONNECT | yes |
| CONNECT | proxy | SOCKS5 | yes |
| CONNECT | direct | direct | yes |
| SOCKS5 | proxy | HTTP CONNECT | yes |
| SOCKS5 | proxy | SOCKS5 | yes |
| SOCKS5 | direct | direct | yes |

Tests must include:

```text
IPv4
IPv6 where CI supports it
domain names
auth success/failure
DNS behavior
timeouts
client disconnect
upstream disconnect
large streaming response
slow response
connection reuse
```

---

## 53. Policy E2E tests

Scenarios:

```text
block domain before upstream connection
route allowed domain to expected pool
direct explicit allowlist
reject private address for untrusted client
sticky session reuses proxy
expired session rotates
quarantined proxy not selected
fallback pool used after eligible failure
budget threshold triggers configured action
shadow policy never changes live result
```

---

## 54. Browser E2E tests

Create a deterministic local test website with:

```text
HTML
CSS
JS
image
font
media-like large endpoint
XHR/fetch endpoint
tracking-like third-party hostname via test DNS/host mapping
WebSocket endpoint
```

Playwright test:

```text
launch -> configure ProxySieve -> attach integration -> navigate
```

Verify:

```text
document succeeds
blocked resources never hit paid/upstream test proxy
XHR remains functional under intended preset
local blocked count reported
traffic counters match expected bytes within protocol overhead rules
session metadata propagated
```

Repeat core behavior for Puppeteer.

Selenium foundation test should at least verify generic proxy connectivity and documented advanced integration path if implemented.

---

## 55. Inspect mode tests

Use locally generated TLS test servers.

Test:

```text
CA creation
leaf certificate generation
trusted client success
untrusted CA failure
include/exclude host matching
full URL rule visibility
content-type rule visibility
certificate cache
pinning/failure policy simulation
disable inspect returns pure tunnel behavior
private key permissions where testable
```

Do not run tests against sensitive third-party services.

---

## 56. Database tests

Required:

```text
fresh database migration
migration from every supported prior schema snapshot
transaction rollback
concurrent read/write behavior
retention aggregation
backup/restore
corrupt/invalid restore rejection
config/schema compatibility
```

Before v1.0, keep fixture databases for at least important pre-release schema checkpoints to test upgrades.

---

## 57. Security tests

Automated tests should cover:

```text
unauthenticated admin rejection
RBAC permission rejection
client auth failure
API key hashing/non-retrievability
secret redaction
URL userinfo redaction
private network denial
DNS rebinding defensive resolution logic where testable
CSRF controls
CORS defaults
login rate limit
oversized headers/requests
malformed proxy protocol inputs
webhook SSRF restrictions if configurable URLs are accepted
path traversal in backup/restore archives
extension manifest/path validation
```

---

## 58. Performance and regression benchmarks

Measure, do not guess.

### 58.1 Core benchmarks

Track:

```text
rule evaluations/sec
rule evaluation latency by rule count
proxy selection latency
traffic accounting overhead
connection throughput
HTTP request throughput
CONNECT tunnel throughput
memory per active tunnel
CPU under concurrency
cache hit path latency
```

### 58.2 Scenario benchmarks

Create reproducible scenarios:

```text
passthrough baseline
browser-lite
browser-aggressive
cache enabled
100/1,000/10,000 rules
100/1,000 proxy endpoints
high session count
```

### 58.3 Traffic-saving benchmarks

For a controlled local test page where exact asset sizes are known, compare:

```text
no ProxySieve filtering
ProxySieve tunnel/domain rules
ProxySieve browser-aware filtering
ProxySieve browser-aware filtering + cache
```

Report:

```text
actual upstream bytes
client bytes
request count
blocked count
cache hit bytes
latency overhead
CPU/memory
```

This is where exact percentage savings can be honestly shown because the test workload is controlled.

---

# Part VIII — GitHub Actions, CI/CD & Releases

## 59. CI workflows

### 59.1 `ci.yml`

Triggers:

```text
pull_request
push to main
```

Jobs:

```text
lint-go
unit-go
race-go
build-go
```

Run with least-privilege token permissions.

### 59.2 `frontend.yml`

Jobs:

```text
frontend-lint
frontend-test
frontend-build
```

Use Node 24 LTS and pinned pnpm setup.

### 59.3 `integration.yml`

Jobs as implementation reaches them:

```text
integration
protocol-matrix
e2e-playwright
e2e-puppeteer
migration-test
```

Do not pass real paid proxy credentials to pull requests from forks.

Tests should primarily use local mock/test proxies.

### 59.4 Security

Prefer GitHub CodeQL default setup initially rather than duplicating a CodeQL workflow unnecessarily. If later advanced setup is required, commit the custom workflow/config deliberately.

Dependency and secret tooling remains enabled through GitHub repository settings/config.

### 59.5 Benchmark workflow

Run lightweight regression benchmarks on PRs that touch hot paths. Run full benchmark suite manually/nightly or on release candidates to control CI cost.

Store benchmark artifacts/results without allowing benchmark noise to block every PR until stable thresholds exist.

---

## 60. GitHub Actions supply-chain rules

Requirements:

- pin action major versions at minimum; consider commit SHA pinning for high-security release workflows;
- `permissions: contents: read` by default;
- grant write permissions only in the release job;
- do not expose repository secrets to untrusted fork workflows;
- avoid executing untrusted PR code in privileged `pull_request_target` jobs;
- Dependabot updates GitHub Actions dependencies too;
- build releases from tags matching controlled patterns.

---

## 61. Release strategy

Use semantic versioning:

```text
0.x during implementation
1.0.0 stable
```

Suggested internal public checkpoints:

```text
v0.1.0 foundation
v0.2.0 gateway
v0.3.0 policies/routing
v0.4.0 pools/sessions
v0.5.0 traffic/health/budgets
v0.6.0 integrations
v0.7.0 API/dashboard
v0.8.0 cache/inspect/extensions
v0.9.0 hardening/release candidate
v1.0.0 stable
```

Codex should not create a release just because code compiles. Each milestone has acceptance gates later in this document.

---

## 62. GoReleaser targets

Use GoReleaser or equivalent reproducible release tooling.

Target artifacts:

```text
linux amd64
linux arm64
windows amd64
windows arm64 when practical
macOS amd64
macOS arm64
```

Output:

```text
proxysieve_<version>_<os>_<arch>.tar.gz/.zip
checksums.txt
SBOM/provenance artifacts where practical
Docker image tags
```

Container tags:

```text
latest
1
1.0
1.0.0
```

Do not move `latest` for pre-release builds.

---

## 63. Release security/provenance

Where supported by the selected GitHub/release flow:

- generate checksums;
- produce SBOM;
- use artifact attestations/provenance;
- minimize release workflow permissions;
- sign releases/artifacts if maintainable;
- document verification steps.

---

## 64. Changelog

Maintain `CHANGELOG.md` using Keep-a-Changelog-like categories:

```text
Added
Changed
Deprecated
Removed
Fixed
Security
```

Release notes can be generated from PR labels but must be curated before stable releases.

---

# Part IX — Documentation & GitHub Presentation

## 65. README requirements

README must answer within the first screen:

```text
What is ProxySieve?
Why would I use it?
How quickly can I run it?
What traffic can it save/control?
What clients does it support?
```

Recommended top structure:

```text
Logo/title
Tagline
Badges
30-second architecture image/text diagram
Quick Start
Why ProxySieve
Key Features
How It Works
Browser-aware vs Gateway-only filtering
Supported proxy/client protocols
Example config
Dashboard screenshot
Benchmarks
Security model
Installation
Documentation
Roadmap
Contributing
License
```

### 65.1 README claim policy

Never write unverifiable marketing such as:

```text
Save 90% bandwidth everywhere
Fastest proxy manager
Undetectable scraping
```

Use measured wording:

```text
In the included controlled browser benchmark, preset X reduced upstream bytes by Y%.
Actual savings depend on workload and policy.
```

### 65.2 Basic quick start

README should eventually support something close to:

```bash
# download/install ProxySieve
proxysieve start
```

Then visit local admin UI, add upstream proxy, test it, attach a pool/policy, and configure client to use `127.0.0.1:8080`.

Include Docker quick start too.

---

## 66. Documentation set

Minimum docs before v1:

```text
architecture.md
security.md
proxy-formats.md
providers.md
pools-and-selection.md
sessions.md
policies.md
traffic-accounting.md
budgets.md
browser-integrations.md
cache.md
inspect-mode.md
extensions.md
api.md or generated API docs
cli.md
configuration.md
deployment.md
backup-restore.md
troubleshooting.md
development.md
benchmarking.md
```

Every dangerous feature requires security notes.

---

## 67. CONTRIBUTING.md

Must explain:

```text
how to build Go backend
how to build dashboard
how to run tests
how to run local test proxies
repository architecture
where public interfaces live
branch/commit conventions
how to add a provider
how to add a selector
how to add a policy action
how to update DB migrations
how to update OpenAPI/generated code
how to run benchmark suite
how to report security issues
```

A fork developer should understand where to add a new feature without reading the entire codebase.

---

## 68. Example-first developer experience

Examples are part of the API quality.

Ship runnable examples for:

```text
curl
Playwright
Puppeteer
Selenium
Python requests/httpx
Node fetch/undici
Go net/http
custom provider extension
custom selector
```

All examples use local/test credentials only.

---

# Part X — Implementation Milestones for Codex

## 69. Codex execution rules

Codex MUST follow these rules throughout the project:

1. Read `PLAN.md` before starting each milestone.
2. Inspect existing code before creating parallel implementations.
3. Do not delete a planned feature because it is difficult; defer within the milestone only when a documented dependency blocks it.
4. Do not add unrelated features outside the plan without an issue/ADR rationale.
5. Implement vertical slices that compile and test.
6. Run relevant tests before every milestone commit/PR.
7. Keep public interfaces small and documented.
8. Never commit real credentials, API keys, certificates or user traffic captures.
9. Never weaken security defaults merely to make a test pass.
10. Do not make DIRECT fallback implicit.
11. Do not fake savings/health metrics.
12. Do not introduce provider brand conditionals into generic modules.
13. Do not allow dashboard code to bypass API/service boundaries.
14. Do not expose third-party dependency types in stable public contracts.
15. Update docs/tests/OpenAPI when behavior changes.
16. Keep repository buildable at the end of every milestone.
17. Use TODO only when it references a concrete remaining milestone/issue; no vague permanent TODOs.
18. Prefer finishing correctness before premature micro-optimization; benchmark hot paths before optimizing them.
19. For protocol/security behavior, fail explicitly rather than guessing.
20. Stop and record an ADR when a decision materially changes the architecture defined here.

---

## 70. Milestone 0 — Repository bootstrap

### Goal

Create a professional, secure, contributor-ready repository skeleton.

### Deliverables

```text
Go module
basic CLI binary
version/build info
README skeleton
PLAN.md
license/community files
Makefile
gitignore/editorconfig
lint configuration
basic CI
Dependabot config
Docker skeleton
web workspace skeleton
```

### Commands that should work

```bash
go test ./...
go build ./cmd/proxysieve
./proxysieve version
```

Frontend:

```bash
cd web
pnpm install
pnpm build
```

### Acceptance criteria

- main binary builds on development OS;
- CI passes;
- no placeholder secret values that resemble live credentials;
- README states current project status honestly;
- repository can be forked and built from documented instructions.

---

## 71. Milestone 1 — Domain contracts, config and storage foundation

### Goal

Create stable core models before network complexity.

### Implement

```text
ProxyEndpoint
ProxySource
Provider contracts
Pool
Session
Client
Policy/Rule
RequestContext
RouteDecision
traffic/cost value types
config schema v1
config precedence
config validation
repository/store interfaces
SQLite connection/migration framework
memory repositories
secret/redaction primitives
```

### Tests

```text
config precedence
invalid config
migration fresh DB
repository CRUD
redaction
value type validation
```

### Acceptance criteria

- core public packages do not import SQLite/UI/provider implementations;
- config has version field;
- migrations run deterministically;
- secrets cannot accidentally appear through normal `String()`/logging helpers where avoidable.

---

## 72. Milestone 2 — Proxy parser, sources and endpoint management

### Goal

Users can import, normalize, validate and manage upstream proxy definitions.

### Implement

```text
proxy URI parser
legacy colon parser
multi-line import
CSV/JSON import mapping
import preview
validation
deduplication
manual source
file source
generic HTTP/API source
source refresh scheduler
provider capability model
proxy CRUD API/service foundation
```

### Tests

- parser table tests for all supported formats;
- fuzz parser;
- ambiguous credentials rejected/warned;
- duplicate handling;
- refresh failure does not delete existing valid endpoints without explicit policy.

### Acceptance criteria

A user can import a sanitized rotating-style proxy string, inspect the normalized endpoint and store it without provider-specific logic leaking into core.

---

## 73. Milestone 3 — HTTP forward + CONNECT gateway

### Goal

ProxySieve functions as a reliable HTTP/HTTPS tunnel gateway.

### Implement

```text
HTTP listener
proxy authentication
HTTP forward requests
CONNECT tunnels
direct transport
HTTP upstream proxy
HTTPS upstream proxy where applicable
SOCKS5 upstream transport
request/connection IDs
timeouts
graceful shutdown
byte accounting hooks
safe destination validation
basic logs
```

### Tests

```text
HTTP direct
HTTP via HTTP upstream
HTTP via SOCKS upstream
CONNECT direct
CONNECT via HTTP proxy
CONNECT via SOCKS
client disconnect
upstream disconnect
timeouts
large streaming data
private destination rejection
```

### Acceptance criteria

- no goroutine leaks in test suite;
- cancellation closes both directions;
- bytes are counted from actual stream operations;
- raw credentials never appear in logs;
- baseline proxy use works with `curl`.

---

## 74. Milestone 4 — SOCKS5 downstream and multi-listener system

### Goal

Generic applications can use HTTP or SOCKS downstream interfaces.

### Implement

```text
SOCKS5 CONNECT listener
SOCKS auth
IPv4/domain/IPv6 targets
multi-listener config
listener-specific client/policy bindings
listener metrics
```

### Tests

Complete protocol matrix relevant to SOCKS.

### Acceptance criteria

A SOCKS5 client can route through HTTP/SOCKS/direct upstream according to service decisions with correct accounting.

---

## 75. Milestone 5 — Policy engine and routing actions

### Goal

Turn the gateway into ProxySieve.

### Implement

```text
condition AST/model
priority ordering
policy composition
ALLOW
BLOCK
REJECT
PROXY pool
DIRECT
SET_TAG
basic THROTTLE
validation for mode-incompatible actions
decision reason codes
debug trace
rule simulator API/CLI
```

Later content-aware actions can attach once inspect/browser metadata exists.

### Tests

```text
priority
stop processing
unknown fields
host/wildcard/regex/IP/CIDR/method/client/listener conditions
security policy precedence
simulator consistency with live evaluator
```

### Acceptance criteria

The same policy evaluator is used by live gateway and simulator. No duplicate rule logic in UI/CLI.

---

## 76. Milestone 6 — Pools, selectors, sessions and failover

### Goal

Reliable multi-proxy routing.

### Implement

```text
pools
pool membership/tags
selection eligibility
all built-in selectors
sticky sessions
TTL/idle/max request/max byte rotation
fallback pools
proxy chains where supported
manual rotate
session API/CLI
```

### Tests

```text
selector determinism where applicable
weighted distribution sanity
session pinning
rotation
fallback
chain error handling
no mid-tunnel rotation
```

### Acceptance criteria

A login-style browser/client session can remain on one proxy across requests and rotate predictably only on configured triggers.

---

## 77. Milestone 7 — Health, circuit breaker and smart retries

### Goal

ProxySieve avoids unhealthy upstreams and limits wasteful retries.

### Implement

```text
passive health metrics
optional active checks
health score/state
quarantine
circuit breaker
half-open probes
retry policy
idempotency/replayability checks
failover integration
health dashboard API data
```

### Tests

```text
state transitions
429/403 configurable weighting
consecutive failures
recovery
retry safety
body replay constraints
retry budget
health-check traffic accounting
```

### Acceptance criteria

A failing proxy is automatically removed from normal candidate selection and can recover through controlled half-open behavior.

---

## 78. Milestone 8 — Traffic analytics, cost and budgets

### Goal

Measure what ProxySieve is actually saving/spending.

### Implement

```text
traffic event aggregation
minute/hour/day rollups
analytics queries
actual byte dimensions
configured cost model
budget scopes
thresholds/actions
projection
retention jobs
live traffic API
```

### Tests

```text
byte correctness
aggregation
retention
cost math
threshold transitions
budget fallback action
high-cardinality handling
```

### Acceptance criteria

Dashboard/API can answer:

```text
How many upstream bytes did client X use today?
Which pool used the most paid traffic?
What is configured estimated cost today?
Which rules blocked requests?
What portion of saved traffic is exact vs estimated?
```

---

## 79. Milestone 9 — Cache and advanced traffic actions

### Goal

Add reusable response optimization without unsafe caching.

### Implement

```text
DNS cache
memory response cache
disk response cache
cache eligibility
TTL/eviction
CACHE action
cache purge API/CLI
response size limits
THROTTLE completion
MOCK/REDIRECT/REWRITE when HTTP is visible
```

### Acceptance criteria

- authenticated/private content is not cached by default;
- cache byte savings are measured separately;
- actions unavailable in tunnel mode produce validation warnings/errors rather than false behavior.

---

## 80. Milestone 10 — Playwright/Puppeteer/Selenium integrations

### Goal

Filter expensive browser resources before they consume upstream proxy traffic.

### Implement

```text
shared integration policy contract
Playwright package
Puppeteer package
Selenium generic guide/foundation
presets
local-block reporting
session/client metadata
policy snapshot refresh
```

### Acceptance criteria

Controlled browser E2E test proves blocked image/media/font/tracker requests do not reach the upstream test proxy when the matching preset is active.

---

## 81. Milestone 11 — Admin API, authentication and RBAC completion

### Goal

Expose a stable management/control plane.

### Implement

```text
full /api/v1 routes
OpenAPI
admin auth
users
roles
clients/API keys
CSRF/CORS/security headers
rate limits
audit log
SSE events
```

### Acceptance criteria

- viewer cannot mutate resources;
- operator cannot perform admin-only secret/security operations;
- API keys shown once;
- OpenAPI matches handlers/models;
- mutation events are auditable.

---

## 82. Milestone 12 — Full React admin dashboard

### Goal

All normal operations can be performed without editing files or using CLI.

### Implement pages listed in section 39.

Critical flows:

```text
first-run setup
add/import proxy
create pool
run test
create/apply policy
simulate policy
configure client
view live traffic
view proxy health
view budgets
manage sessions
manage providers/sources
manage alerts
settings
```

### Acceptance criteria

A new user can complete the first-run flow using only the browser UI after starting the binary.

---

## 83. Milestone 13 — Shadow policies, events, alerts and extensions

### Goal

Make the product safely customizable and operational.

### Implement

```text
shadow evaluation/comparison
event bus hardening
webhook alerts
extension manifest/protocol v1
extension process lifecycle
example provider
example selector
example alert sink
extension API/docs
```

### Acceptance criteria

A sample external extension can run out-of-process, negotiate API v1, perform its documented capability and fail without crashing the gateway.

---

## 84. Milestone 14 — HTTPS Inspect Mode

### Goal

Offer advanced content-aware filtering for explicitly selected HTTPS targets.

### Implement section 29 completely.

### Acceptance criteria

- OFF by default;
- CA key protected;
- include/exclude works;
- inspected HTTPS requests can match path/header/content-type policy conditions;
- excluded host remains a tunnel;
- failure policy deterministic;
- UI shows prominent security status.

---

## 85. Milestone 15 — Backup/import/export and operations

### Goal

Make ProxySieve easy to move, fork, recover and operate.

### Implement

```text
config export/import
policy export/import
portable archive
backup
restore validation
DB compact/status
doctor command
system diagnostics
```

### Acceptance criteria

A fresh install can restore a sanitized backup and reproduce pools, policies, settings and analytics state as documented.

---

## 86. Milestone 16 — Hardening, benchmarks and v1.0 release

### Goal

Turn the accumulated features into a credible stable release.

### Required work

```text
race/fuzz test review
security test suite
load tests
protocol matrix
browser E2E
migration tests
benchmark publication
disk/memory retention tuning
error-message review
CLI help review
API compatibility review
README completion
documentation completion
sample configs
Docker hardening
cross-platform release test
SBOM/checksums/provenance
```

### Release candidate

Create at least one pre-release candidate such as:

```text
v1.0.0-rc.1
```

Resolve all P0/P1 correctness/security defects before `v1.0.0`.

---

# Part XI — Definition of Done for v1.0

## 87. Functional definition of done

v1.0 is complete only when all of these are true:

- HTTP forward proxy works;
- HTTPS CONNECT works;
- SOCKS5 downstream works;
- HTTP/HTTPS/SOCKS upstream paths work where documented;
- static and rotating proxy sources are represented;
- multiple proxy formats import safely;
- pools and selectors work;
- sessions and rotation work;
- health/circuit-breaker/failover work;
- policy engine is deterministic and explainable;
- generic clients work without SDK;
- Playwright and Puppeteer integrations demonstrably block upstream traffic before it is sent;
- actual traffic accounting works;
- estimated saving is clearly distinguished;
- cost/budget controls work;
- cache works with safe defaults;
- shadow policy works;
- optional inspect mode works for documented scenarios;
- API is versioned and documented;
- CLI covers core administration;
- dashboard covers first-run and normal operations;
- auth/RBAC/API keys work;
- private network/security defaults work;
- alerts/webhooks work;
- backup/restore/import/export work;
- extension API v1 and examples work;
- Linux/Windows/macOS artifacts build;
- Docker image builds/runs;
- CI/security scans pass;
- docs/examples are complete enough for a new user.

---

## 88. Quality definition of done

Before v1.0:

```text
go test ./... passes
go test -race ./... passes on supported CI target
frontend tests pass
linters pass
protocol integration matrix passes
browser E2E passes
DB migration tests pass
security critical tests pass
release build succeeds
no known P0/P1 defect
no known plaintext secret leak path
no open proxy by default
```

Performance thresholds should be based on collected baselines. Any significant regression from the accepted release candidate baseline must be investigated.

---

## 89. Compatibility definition of done

Document:

```text
minimum Go version for contributors
supported OS/architectures
supported downstream protocols
supported upstream protocols
browser integration package versions/ranges
config schema version
API version
extension API version
SQLite backup compatibility expectations
```

Stable `v1.x` should avoid breaking public API/config behavior without deprecation and migration guidance.

---

# Part XII — Initial GitHub Setup Checklist for the Maintainer

## 90. Before giving the repository to Codex

Maintainer checklist:

- [ ] Decide GitHub owner/organization.
- [ ] Create `proxysieve` public repository, preferably empty.
- [ ] Confirm the repository name has no known conflicting project identity.
- [ ] Add the description and topics from section 1.
- [ ] Enable Issues.
- [ ] Enable Discussions.
- [ ] Enable automatic branch deletion after merge.
- [ ] Enable squash merge; disable merge commits/rebase merge if following this plan.
- [ ] Set default Actions token permissions to restricted/read where possible.
- [ ] Enable Dependabot alerts/security updates.
- [ ] Commit `.github/dependabot.yml` for version updates.
- [ ] Enable CodeQL default setup after Go/JS code exists.
- [ ] Enable secret scanning/push protection where available.
- [ ] Create `Protect main` ruleset.
- [ ] Initially protect deletion/force-push and require PR.
- [ ] After CI has run, add required status checks.
- [ ] Add maintainer CODEOWNERS.
- [ ] Add issue forms and PR template.
- [ ] Add Apache-2.0 LICENSE.
- [ ] Never commit the real proxy credential used during brainstorming/testing.

---

## 91. Recommended first GitHub issues/milestones

Create milestones matching Milestones 0–16.

Create a few high-level tracking issues instead of hundreds of empty tasks immediately:

```text
[M0] Repository bootstrap
[M1] Domain/config/storage foundation
[M2] Proxy normalization and sources
[M3] HTTP/CONNECT gateway
[M4] SOCKS5 downstream
[M5] Policy engine
[M6] Pools/sessions/chaining
[M7] Health/retries/failover
[M8] Traffic/cost/budgets
[M9] Cache/advanced actions
[M10] Browser integrations
[M11] API/auth/RBAC
[M12] Admin dashboard
[M13] Shadow/events/extensions
[M14] HTTPS Inspect Mode
[M15] Operations/backup/import-export
[M16] Hardening and v1.0 release
```

Codex may break a milestone into smaller issues as implementation details become clear.

---

## 92. Recommended GitHub Project board

Optional but useful columns/statuses:

```text
Backlog
Ready
In Progress
In Review
Blocked
Done
```

Custom fields:

```text
Milestone
Area
Priority
Risk
```

Keep GitHub Issues as source of truth; do not duplicate task descriptions into a separate undocumented tracker.

---

# Part XIII — First Run UX Target

## 93. Zero-config startup

After installation:

```bash
proxysieve start
```

Expected console concept:

```text
ProxySieve v1.0.0

Gateway
  HTTP    127.0.0.1:8080
  SOCKS5  127.0.0.1:1080

Admin
  http://127.0.0.1:9090

Storage
  ~/.proxysieve/proxysieve.db

Inspect mode
  disabled

Status
  ready
```

If admin first-run setup is required, print a one-time setup URL/token without logging it to persistent logs.

---

## 94. First useful workflow

A new user should be able to:

1. start ProxySieve;
2. open dashboard;
3. import one upstream proxy in any common supported format;
4. preview normalized result;
5. save and test it;
6. create/use default pool;
7. apply `passthrough` or `browser-lite` preset;
8. point client at `127.0.0.1:8080` or SOCKS port;
9. see traffic live;
10. see upstream bytes, status, proxy used and matched policy;
11. optionally install Playwright/Puppeteer integration for deeper filtering.

This flow is a release-blocking E2E scenario.

---

# Part XIV — Architecture Decisions to Record as ADRs

## 95. Initial ADR set

Codex should create lightweight ADRs under:

```text
docs/adr/
```

At minimum:

```text
0001-go-as-core-language.md
0002-sqlite-default-storage.md
0003-public-api-and-dashboard-boundary.md
0004-policy-engine-semantics.md
0005-non-mitm-default.md
0006-external-extension-protocol.md
0007-traffic-accounting-model.md
0008-security-private-network-default.md
```

ADR format:

```text
Context
Decision
Consequences
Alternatives considered
```

Do not write essay-length ADRs; record decisions that future fork maintainers need to understand.

---

# Part XV — Future Expansion After v1 Without Breaking the Model

## 96. Planned-compatible future directions

The v1 architecture should leave clean paths for:

```text
PostgreSQL storage adapter
Redis/distributed cache adapter
NATS/Kafka event adapter
multi-node control plane
remote worker gateways
TUN/transparent proxy listener
SOCKS5 UDP
HTTP/3/QUIC-aware features where feasible
organization/custom RBAC
provider extension registry
hosted dashboard/control plane
additional browser automation integrations
Grafana dashboards
native OS keychain integrations
advanced cost/quota provider APIs
```

These are not excuses to over-engineer v1. Interfaces should be introduced only where a current v1 boundary already exists.

---

# Part XVI — Final Instructions to Codex

## 97. What Codex should do first

When this plan is handed to Codex in a new repository, execute in this order:

```text
1. Read PLAN.md completely.
2. Inspect repository state and Git history.
3. Confirm module path/owner from git remote; do not invent a different owner.
4. Create/refresh a milestone checklist in the repository issue/task context.
5. Implement Milestone 0 only.
6. Run formatter/lint/tests/build.
7. Fix failures.
8. Commit with Conventional Commit message.
9. Continue to Milestone 1.
10. Never jump directly to dashboard before gateway/domain foundations exist.
```

If GitHub remote credentials and permissions are available, push completed milestone branches/commits according to the repository workflow. Do not rewrite `main` history.

---

## 98. What Codex must never do

Do not:

```text
hardcode the example proxy credential;
commit live credentials;
turn ProxySieve into an open public proxy by default;
claim HTTPS path/resource visibility in tunnel mode;
 silently direct-route failed proxy traffic;
mark estimated bytes as actual savings;
retry non-idempotent requests blindly;
make provider brands part of core domain logic;
use Go native plugin as the only extension mechanism;
make React depend on SQLite;
make SQLite types part of public core interfaces;
use unversioned API/config/extension formats;
disable tests/security checks to get green CI;
create dozens of empty abstractions without current boundaries;
introduce Kubernetes/Redis/Postgres as mandatory v1 dependencies;
add scraping-evasion/CAPTCHA/fingerprint-bypass features outside this product scope;
```

---

## 99. Decision hierarchy when the plan is ambiguous

When a lower-level implementation detail is not specified, prioritize in this order:

```text
1. security/correctness
2. explicit product behavior in PLAN.md
3. protocol standards/interoperability
4. user experience/easy installation
5. modularity/forkability
6. observability
7. performance based on measurement
8. implementation convenience
```

Record major deviations in an ADR and update this plan/documentation rather than silently diverging.

---

# Part XVII — Recommended v1 Positioning

## 100. Product pitch

Long form:

> ProxySieve is a self-hosted smart traffic gateway for paid proxies. Put it between your application and residential, datacenter or rotating proxy infrastructure to filter unnecessary traffic, route requests through the right proxy pool, keep sticky sessions, avoid unhealthy endpoints, enforce traffic and cost budgets, and understand where every upstream byte goes. Generic applications can use it as a normal HTTP/SOCKS proxy, while Playwright and Puppeteer integrations can block expensive browser resources before they ever enter the paid proxy path.

Short form:

> Smart traffic control for paid proxies.

GitHub hook:

> Stop paying for bytes you don't need.

Supporting line:

> Route smarter, filter earlier, measure everything.

---

## 101. Why the project is different

The README and documentation should emphasize the combination, not any single feature:

```text
universal proxy gateway
+
application-aware browser filtering
+
proxy pool/session management
+
quality/failover engine
+
traffic and cost accounting
+
programmable policy engine
+
self-hosted admin UI/API/CLI
+
extension-first architecture
```

That combination is the core identity of ProxySieve.

---

# Appendix A — Suggested default policy example

```yaml
version: 1
name: browser-lite
rules:
  - name: deny unsafe private destinations for untrusted clients
    priority: 10000
    conditions:
      client_trust: untrusted
      destination_class: private_or_link_local
    actions:
      - type: reject

  - name: block known trackers
    priority: 9000
    conditions:
      host:
        any:
          - "*.doubleclick.net"
          - "*.google-analytics.com"
    actions:
      - type: block

  - name: block media from browser integration
    priority: 8000
    conditions:
      resource_type:
        any:
          - media
    actions:
      - type: block

  - name: default paid proxy route
    priority: 100
    actions:
      - type: proxy
        pool: default
```

This is illustrative. Built-in tracker lists/presets need their own maintenance strategy and should not become giant hardcoded core tables.

---

# Appendix B — Example rotating proxy source concept

The project must support strings conceptually similar to:

```text
proxy.example.invalid:10000:user-country-ca-os-windows-session-demo-lifetime-10:password
```

but repository fixtures must use fake domains and fake secrets.

Normalized object concept:

```yaml
protocol: http
host: proxy.example.invalid
port: 10000
credentials:
  username_ref: secret://proxy/demo/username
  password_ref: secret://proxy/demo/password
provider: generic
metadata:
  country: ca
  os: windows
  session: demo
  lifetime_minutes: 10
```

Provider-specific parsing of structured usernames belongs in an adapter/template, not generic gateway logic.

---

# Appendix C — Suggested local development commands

Target convenient developer commands via Makefile/scripts:

```bash
make bootstrap
make fmt
make lint
make test
make race
make build
make web
make integration
make e2e
make benchmark
make docker
make run
```

`make bootstrap` should install/check development tooling without hiding what is installed.

Where possible, also document equivalent raw Go/pnpm commands so contributors are not trapped behind Make.

---

# Appendix D — GitHub ruleset rollout order

Recommended practical sequence for a brand-new solo-maintained repo:

```text
Step 1
Create repo and first bootstrap commit.

Step 2
Enable main ruleset:
- block force push
- block deletion
- PR requirement
- linear history
- maintainer/admin bypass available for bootstrap

Step 3
Push CI workflows and let them run.

Step 4
Add observed CI check names as required status checks.

Step 5
Enable CodeQL default setup and security features.

Step 6
Once another maintainer exists, require at least one approval and CODEOWNER approval for sensitive paths.
```

This avoids creating a protection rule that blocks the repository before the required CI status contexts exist.

---

# Appendix E — Launch checklist for `v1.0.0`

- [ ] All milestone acceptance criteria complete.
- [ ] `main` green.
- [ ] CodeQL/security alerts reviewed.
- [ ] Dependabot critical/high alerts resolved or documented.
- [ ] Race suite green.
- [ ] Browser E2E green.
- [ ] Protocol matrix green.
- [ ] Migration/backup restore green.
- [ ] Cross-platform release artifacts tested.
- [ ] Docker image tested as non-root where practical.
- [ ] README quick start tested from a clean machine/container.
- [ ] No real proxy secrets in Git history.
- [ ] Public example credentials are fake.
- [ ] Admin defaults bind to loopback.
- [ ] Inspect mode is OFF by default.
- [ ] DIRECT is not an implicit fallback.
- [ ] Security documentation complete.
- [ ] Benchmark results reproducible and claims accurate.
- [ ] API/config/extension versions documented.
- [ ] CHANGELOG finalized.
- [ ] Release notes finalized.
- [ ] Tag `v1.0.0` from reviewed `main` commit.
- [ ] Publish binaries/checksums/container/SBOM or planned provenance artifacts.

---

# Final project rule

ProxySieve should remain easy to understand at the boundary:

```text
Client -> ProxySieve -> Decision -> Upstream/Direct/Block/Cache -> Client
```

Complexity belongs in isolated modules behind explicit contracts. The default user should be able to install and benefit from ProxySieve in minutes; the advanced user should be able to customize almost every routing, filtering, session, provider, accounting and observability behavior; and a developer should be able to fork the repository and replace individual components without rewriting the network core.

**End of implementation plan.**
