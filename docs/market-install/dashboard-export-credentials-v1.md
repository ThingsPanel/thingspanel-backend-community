# Dashboard analysis/export credential boundary

Existing owner routes are ThingsVis public `/api/v1/market-dashboards/:id/analyze` and `/export`, which invoke authorizeMarketInternalRequest and getSessionUser for a Bearer request, then load only a dashboard in that authenticated ThingsVis tenant. This patch does not alter that owner or enable its shared internal-token path.

The platform browser request wrapper owns Authorization and x-token for platform authentication. Dashboard analysis/publication therefore carries its separate ThingsVis public-session bearer in `X-ThingsVis-Authorization`; its personal Horizon token remains in the established publish body. The platform handler extracts ThingsVis authorization explicitly before calling Analyze/Export. Missing/invalid source authorization fails before owner calls, even if another client has an internal token configured.

Compatibility: old direct clients may continue the explicit pair `x-token: <platform credential>` and `Authorization: Bearer <different ThingsVis credential>`. The ordinary mirrored platform Authorization value is rejected as an implicit source token. No platform authentication header is rewritten. Source authorization is still verified by ThingsVis; this helper only selects the explicitly supplied transport.

Before creating a local publication reference or sending its POST, PublishWizard prepares the ThingsVis session credential. A failure at this local preflight is known not sent and leaves no publication intent. Once the publication POST is sent, errors retain the stable key and use read-only receipt query as documented separately. This distinction avoids permanently holding an operation that was never submitted merely because the source login expired.

Tests:

```sh
go test -race -count=1 -v ./internal/api -run TestDashboardExportKeepsPlatformAndThingsVisCredentialsSeparate
go test -race -count=1 -v ./internal/service -run TestThingsVisClient_AnalyzeMarketDashboard
```

Frontend tests extend the real API module and PublishWizard SFC to verify the independent header, source-token preparation before POST, and that receipt queries do not request source credentials. These are module tests; real deployed dual-token browser/HTTP behavior remains part of fixed-version integration acceptance, not inferred from fixture success.
