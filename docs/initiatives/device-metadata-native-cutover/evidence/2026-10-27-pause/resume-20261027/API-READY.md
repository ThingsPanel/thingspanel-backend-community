# Owned backend API ready — 2026-10-27

- API: `http://127.0.0.1:55067`
- Image: `codex-device-management-20261027-7312c5b5:resume-20261027`
- Image ID: `sha256:ac61bf0cc4659afdae59df8c466a5436e5b3b902649c636b3c989a3801e22ca2`
- Complete private runtime profile: `/Volumes/DevScratch/device-management-native-20261027/luna-http-7312c5b5/resume-20261027/build-infra.private.json`
- Synthetic auth fixtures (private; do not print values): `/Volumes/DevScratch/device-management-native-20261027/luna-http-7312c5b5/resume-20261027/auth-fixtures.private.json`
- For separate UI work use tenant B with `tenantBDeviceId` from `/Volumes/DevScratch/device-management-native-20261027/luna-http-7312c5b5/http-fixture-ids.private.json`.
- Session validation uses only the owned local auth stub. No real identity-provider integration is claimed.
- The added logical databases are empty test databases. The object-storage endpoint is intentionally unavailable and was not accessed.
- Credentials created by HTTP acceptance are pending; activation and runtime connection are outside this slice.
