"""
Playwright E2E test: verify that SSO (user role) and Dev Login (admin role)
see different content on the Settings page.

After task-zone frontend refactor:
- Dev login (admin): Settings page accessible, Admin Console task zones visible
- SSO user (user role): Admin links hidden in sidebar, /settings and /admin/users redirect to /

Usage:
  python frontend/e2e_role_test.py
  AE_E2E_BASE_URL=http://127.0.0.1:PORT npm run test:e2e:role
"""

import sys
import os
import json
from urllib.parse import parse_qs, urlparse
from playwright.sync_api import sync_playwright

BASE = os.environ.get("AE_E2E_BASE_URL", "http://localhost:5173").rstrip("/")
API = "http://localhost:8081/api/v1"
SCREENSHOT_DIR = "/tmp/ae-e2e-role"

passed = 0
failed = 0
errors = []


def screenshot(page, name):
    page.screenshot(path=f"{SCREENSHOT_DIR}/{name}.png", full_page=True)


def report(name, ok, detail=""):
    global passed, failed
    if ok:
        passed += 1
        print(f"  ✅ {name}")
    else:
        failed += 1
        errors.append((name, detail))
        print(f"  ❌ {name}: {detail}")


def clear_auth_routes(page):
    for pattern in [
        "**/api/v1/auth/options",
        "**/api/v1/auth/dev-login",
        "**/api/v1/auth/refresh",
        "**/api/v1/auth/me",
        "**/api/v1/efficiency/dashboard",
        "**/api/v1/user/providers",
        "**/api/v1/work-items/counts",
        "**/api/v1/events**",
        "**/api/v1/attribution/report**",
        "**/api/v1/activity/summary**",
        "**/api/v1/activity/members/**",
        "**/api/v1/activity/buckets/**",
        "**/api/v1/scm-providers**",
        "**/api/v1/admin/providers**",
        "**/api/v1/admin/credentials**",
        "**/api/v1/system/version**",
        "**/api/v1/admin/settings/ldap**",
    ]:
        try:
            page.unroute(pattern)
        except Exception:
            pass


def mock_auth_endpoints(page, role="admin"):
    clear_auth_routes(page)

    page.route("**/api/v1/auth/options", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({
            "code": 0,
            "data": {
                "ldap_enabled": True,
                "dev_login_enabled": True,
            },
        }),
    ))

    page.route("**/api/v1/auth/dev-login", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({
            "code": 0,
            "data": {
                "token": f"{role}-token",
                "refresh_token": f"{role}-refresh",
            },
        }),
    ))

    page.route("**/api/v1/auth/refresh", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({
            "code": 0,
            "data": {
                "token": f"{role}-token-refreshed",
                "refresh_token": f"{role}-refresh",
            },
        }),
    ))

    page.route("**/api/v1/auth/me", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({
            "code": 0,
            "data": {
                "id": 1 if role == "admin" else 999,
                "username": "admin" if role == "admin" else "sso_test_user",
                "email": "admin@example.com" if role == "admin" else "alice@example.com",
                "role": role,
                "auth_source": "dev" if role == "admin" else "sso",
            },
        }),
    ))

    page.route("**/api/v1/efficiency/dashboard", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"code": 0, "data": {"total_repos": 0, "tracked_workflows": 0, "total_ai_prs": 0}}),
    ))
    page.route("**/api/v1/user/providers", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"code": 0, "data": {"providers": []}}),
    ))
    page.route("**/api/v1/work-items/counts", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"code": 0, "data": {}}),
    ))
    activity_member = {
        "contract_version": "activity-v1",
        "window": {"from": "2026-07-09T00:00:00Z", "to": "2026-08-08T00:00:00Z"},
        "member": {
            "user_id": 7,
            "display_name": "Alice",
            "email": "alice@example.com",
            "department_external_ids": ["team-alpha"],
        },
        "available": True,
        "metrics": {
            "participating_prs": {"value": 2, "lower_bound": True},
            "merged_prs": {"value": 1, "lower_bound": True},
            "active_repositories": 1,
            "commit_count": 1,
            "latest_activity": "2026-08-05T12:00:00Z",
        },
        "quality": {
            "measured_buckets": 1,
            "unbound_buckets": 0,
            "multi_repo_shared_buckets": 0,
            "invalid_token_facts": 0,
            "historical_advisory_facts": 0,
            "coverage_gap_count": 0,
        },
        "sync_coverage": {
            "complete": False,
            "affected_repositories": 1,
            "unsynced_repositories": 1,
            "stale_repositories": 0,
            "partially_synced_repositories": 0,
            "failed_repositories": 0,
        },
        "prs": {
            "items": [{
                "repo_config_id": 9,
                "repo_name": "example-org/repo-a",
                "pr_record_id": 21,
                "scm_pr_id": 88,
                "title": "Improve activity",
                "url": "https://example.com/pull/88",
                "status": "merged",
                "commits": [{"repo_config_id": 9, "commit_sha": "abcdef123456"}],
            }],
        },
        "commits": {
            "items": [{
                "repo_config_id": 9,
                "repo_name": "example-org/repo-a",
                "commit_sha": "abcdef123456",
                "latest_activity": "2026-08-05T12:00:00Z",
                "processed_tokens": 1234,
                "prs": [{"repo_config_id": 9, "pr_record_id": 21, "scm_pr_id": 88}],
            }],
        },
        "buckets": {
            "items": [{
                "bucket_id": "bucket-e2e",
                "observed_end_at": "2026-08-05T12:00:00Z",
                "processed_tokens": 1234,
                "allocation_status": "bound_auto",
            }],
        },
        "bucket_access": role == "admin",
    }
    page.route("**/api/v1/activity/summary**", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"code": 0, "data": activity_member}),
    ))
    page.route("**/api/v1/activity/members/**", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"code": 0, "data": activity_member}),
    ))
    page.route("**/api/v1/activity/buckets/**", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({
            "code": 0,
            "data": {
                "contract_version": "activity-v1",
                "bucket_id": "bucket-e2e",
                "owner_user_id": 7,
                "tool": "codex",
                "model": "gpt-5",
                "observed_start_at": "2026-08-05T11:00:00Z",
                "observed_end_at": "2026-08-05T12:00:00Z",
                "tokens": {
                    "fresh_input_tokens": 100,
                    "cache_read_tokens": 200,
                    "cache_write_tokens": 300,
                    "output_tokens": 400,
                    "reasoning_tokens": 50,
                    "provider_total_tokens": 1000,
                    "processed_total_tokens": 1000,
                },
                "token_quality": "complete",
                "coverage_gap_count": 0,
                "extractor_version": "codex-v2",
                "normalization_version": 3,
                "correlation_quality": "request_id",
                "revision": {
                    "revision_id": "revision-e2e",
                    "sequence": 2,
                    "reason": "commit_evidence",
                    "evidence_version": "v2",
                    "restated_at": "2026-08-05T12:01:00Z",
                    "allocations": [],
                },
                "request_ids": {
                    "state": "retained",
                    "count": 1,
                    "evidence": [{
                        "request_id": "req_e2e",
                        "observed_at": "2026-08-05T11:30:00Z",
                        "transport": "responses",
                        "failed": False,
                    }],
                },
            },
        }),
    ))
    page.route("**/api/v1/events**", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"code": 0, "data": {"items": [], "total": 0, "page": 0, "page_size": 3}}),
    ))
    page.route("**/api/v1/scm-providers**", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"code": 0, "data": {"items": [], "total": 0, "page": 1, "page_size": 20}}),
    ))
    page.route("**/api/v1/admin/providers**", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"code": 0, "data": []}),
    ))
    page.route("**/api/v1/admin/credentials**", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"code": 0, "data": []}),
    ))
    page.route("**/api/v1/system/version**", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({
            "code": 0,
            "data": {
                "version": {"version": "v0.0.0-test", "commit": "test", "build_time": "2026-01-01T00:00:00Z"},
                "check_enabled": True,
                "checked": route.request.method == "POST",
                "update_available": False,
                "latest_release": {"version": "v0.0.0-test", "url": "https://example.com/releases/v0.0.0-test"},
            },
        }),
    ))
    page.route("**/api/v1/admin/settings/ldap**", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"code": 0, "data": {"url": "", "base_dn": "", "bind_dn": "", "user_filter": "", "tls": False}}),
    ))


def do_dev_login(page, role="admin"):
    """Seed an authenticated session with the requested role for role-gating checks."""
    mock_auth_endpoints(page, role)
    page.goto(f"{BASE}/login")
    page.wait_for_load_state("networkidle")
    page.evaluate(
        """([token, refresh]) => {
            localStorage.setItem('token', token)
            localStorage.setItem('refresh_token', refresh)
        }""",
        [f"{role}-token", f"{role}-refresh"],
    )
    page.goto(BASE)
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(1000)


def do_logout(page):
    """Logout via sidebar button."""
    page.goto(BASE)
    page.wait_for_load_state("networkidle")
    logout_btn = page.locator("button[title='Logout']")
    if logout_btn.is_visible():
        logout_btn.click()
        page.wait_for_load_state("networkidle")
        page.wait_for_timeout(500)
    clear_auth_routes(page)


def test_dev_login_settings(page):
    """Test: Dev login (admin) can see SCM Providers and LLM config on Settings page."""
    print("\n🧪 Dev Login (admin) — Settings Page")

    do_dev_login(page)
    screenshot(page, "01_dev_login_dashboard")

    # Verify admin role in sidebar
    page.goto(BASE)
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)
    sidebar_text = page.locator("aside").inner_text()
    report("Dev login user role is admin",
           "admin" in sidebar_text,
           f"Sidebar text: {repr(sidebar_text[:200])}")

    # Settings link should be visible for admin
    settings_link = page.locator("aside a[href='/settings']")
    report("Settings link visible in sidebar for admin",
           settings_link.is_visible())

    # Navigate to settings
    page.goto(f"{BASE}/settings")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(1000)
    screenshot(page, "02_dev_login_settings")

    # Should stay on /settings (not redirected)
    report("Admin stays on /settings",
           "/settings" in page.url,
           f"URL: {page.url}")

    report("Admin Console heading visible",
           page.locator("h1:has-text('Admin Console')").is_visible())
    report("AI Services tab visible",
           page.locator("[data-testid='settings-tab-ai-services']").is_visible())
    report("Code Platforms tab visible",
           page.locator("[data-testid='settings-tab-code-platforms']").is_visible())

    page.locator("[data-testid='settings-tab-code-platforms']").click()
    code_platforms_heading = page.locator("h2:has-text('Code Platforms')")
    add_platform_button = page.locator("button:has-text('Add Platform')")
    code_platforms_heading.wait_for(state="visible")
    add_platform_button.wait_for(state="visible")
    report("Code Platforms section visible",
           code_platforms_heading.is_visible())
    report("Add Platform button visible",
           add_platform_button.is_visible())

    page.locator("[data-testid='settings-tab-deployment-runtime']").click()
    page.locator("h2:has-text('Deployment & Runtime')").wait_for(state="visible")
    report("Deployment & Runtime section visible",
           page.locator("h2:has-text('Deployment & Runtime')").is_visible())
    report("Current version visible",
           page.locator("text=v0.0.0-test").first.is_visible())
    report("Check Updates button visible",
           page.locator("button:has-text('Check Updates')").is_visible())
    report("Restart Service button removed",
           page.locator("button:has-text('Restart Service')").count() == 0,
           "Restart Service button is still visible")

    do_logout(page)


def test_user_role_settings_blocked(page):
    """
    Test: User role cannot access Settings page.
    - Settings link hidden in sidebar
    - /settings route redirects to /
    """
    print("\n🧪 User Role — Settings Page Blocked")

    do_dev_login(page, role="user")

    # Refresh to pick up the mocked /me response
    page.goto(BASE)
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(1000)
    screenshot(page, "03_user_role_dashboard")

    # Verify sidebar shows "user" role
    sidebar_text = page.locator("aside").inner_text()
    report("User role shown in sidebar",
           "user" in sidebar_text,
           f"Sidebar text: {repr(sidebar_text[:200])}")

    # Settings link should be HIDDEN for user role
    settings_link = page.locator("aside a[href='/settings']")
    report("Settings link hidden in sidebar for user role",
           settings_link.count() == 0 or not settings_link.is_visible(),
           "Settings link is still visible")

    # Navigate to /settings directly — should redirect to /
    page.goto(f"{BASE}/settings")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(1000)
    screenshot(page, "04_user_role_settings_redirect")

    report("User role redirected away from /settings",
           "/settings" not in page.url,
           f"URL: {page.url}")

    # Clean up
    do_logout(page)


def test_user_role_admin_users_blocked(page):
    """Test: User role cannot access /admin/users either."""
    print("\n🧪 User Role — /admin/users Blocked")

    do_dev_login(page, role="user")
    page.goto(BASE)
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)

    # Navigate to /admin/users — should redirect
    page.goto(f"{BASE}/admin/users")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(1000)

    report("User role redirected away from /admin/users",
           "/admin/users" not in page.url,
           f"URL: {page.url}")

    do_logout(page)


def test_activity_route_layout_and_responsive_style(page):
    """Protected Activity returns after login and renders inside the shared shell."""
    print("\n🧪 Activity — Route, Layout, and Responsive Style")

    mock_auth_endpoints(page, role="admin")
    page.goto(f"{BASE}/login")
    page.evaluate("localStorage.clear()")

    page_errors = []
    on_page_error = lambda error: page_errors.append(str(error))
    page.on("pageerror", on_page_error)

    page.goto(f"{BASE}/activity")
    page.wait_for_load_state("networkidle")
    report("Protected Activity preserves the requested redirect",
           page.url == f"{BASE}/login?redirect=/activity",
           f"URL: {page.url}")

    page.locator("button:has-text('Dev Login')").click()
    page.wait_for_timeout(600)
    report("Dev login returns to /activity",
           page.url == f"{BASE}/activity",
           f"URL: {page.url}")

    if page.url != f"{BASE}/activity":
        page.goto(f"{BASE}/activity")
    page.wait_for_timeout(800)

    sidebar = page.locator("aside")
    activity_link = page.locator("aside a[href='/activity']")
    report("Activity uses the shared desktop app shell",
           sidebar.count() == 1 and sidebar.is_visible(),
           f"aside count: {sidebar.count()}")
    report("Activity navigation is active",
           activity_link.count() == 1 and "bg-gray-800" in (activity_link.get_attribute("class") or ""),
           f"class: {activity_link.get_attribute('class') if activity_link.count() else None}")
    report("Activity renders PR-first lower-bound metrics",
           "≥2" in page.locator("[data-testid='activity-hero']").inner_text()
           and page.locator("[data-testid='activity-prs']").count() == 1)
    report("Activity data does not crash the page",
           not page_errors,
           repr(page_errors))
    report("Activity page has no desktop horizontal overflow",
           page.evaluate("document.documentElement.scrollWidth <= window.innerWidth"))
    screenshot(page, "05_activity_desktop")

    page.goto(
        f"{BASE}/attribution"
        "?from=2026-08-01T00%3A00%3A00Z"
        "&to=2026-08-08T00%3A00%3A00Z"
        "&unsafe=discard-me"
    )
    page.wait_for_url("**/activity?*")
    legacy_url = urlparse(page.url)
    legacy_query = parse_qs(legacy_url.query)
    report("Legacy attribution route redirects to Activity with safe range state",
           legacy_url.path == "/activity"
           and legacy_query == {
               "from": ["2026-08-01T00:00:00Z"],
               "to": ["2026-08-08T00:00:00Z"],
           },
           f"URL: {page.url}")

    page.set_viewport_size({"width": 390, "height": 844})
    page.reload()
    page.wait_for_timeout(800)
    report("Activity uses the shared mobile header",
           page.locator("header button:has-text('Menu')").is_visible())
    report("Activity page has no mobile horizontal overflow",
           page.evaluate("document.documentElement.scrollWidth <= window.innerWidth"))
    screenshot(page, "06_activity_mobile")

    page.remove_listener("pageerror", on_page_error)
    page.set_viewport_size({"width": 1440, "height": 900})
    page.evaluate("localStorage.clear()")
    clear_auth_routes(page)


def test_activity_member_bucket_authorization(page):
    """Representative member views omit Bucket data while Admin can load it lazily."""
    print("\n🧪 Activity — Member Bucket Authorization")

    do_dev_login(page, role="user")
    page.goto(f"{BASE}/activity/members/7")
    page.wait_for_load_state("networkidle")
    report("Representative member view renders the authorized member",
           page.locator("h1:has-text('Alice')").count() == 1)
    report("Representative member view does not render Bucket rows",
           page.locator("[data-testid='activity-buckets']").count() == 0)
    do_logout(page)

    do_dev_login(page, role="admin")
    page.goto(f"{BASE}/activity/members/7")
    page.wait_for_load_state("networkidle")
    bucket = page.locator("[data-testid='activity-bucket-bucket-e2e']")
    report("Admin member view renders restricted Bucket rows",
           bucket.count() == 1 and bucket.is_visible())
    bucket.click()
    page.wait_for_timeout(300)
    detail = page.locator("[data-testid='activity-bucket-detail-bucket-e2e']")
    report("Admin loads retained Request ID evidence only after expansion",
           detail.count() == 1 and "req_e2e" in detail.inner_text())
    do_logout(page)


def run_all():
    global passed, failed

    os.makedirs(SCREENSHOT_DIR, exist_ok=True)

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1440, "height": 900})
        page = context.new_page()

        tests = [
            ("Admin (Dev Login) Settings", lambda: test_dev_login_settings(page)),
            ("User Role Settings Blocked", lambda: test_user_role_settings_blocked(page)),
            ("User Role /admin/users Blocked", lambda: test_user_role_admin_users_blocked(page)),
            ("Activity Route and Layout", lambda: test_activity_route_layout_and_responsive_style(page)),
            ("Activity Member Bucket Authorization", lambda: test_activity_member_bucket_authorization(page)),
        ]

        for name, fn in tests:
            try:
                fn()
            except Exception as e:
                failed += 1
                errors.append((name, str(e)))
                print(f"  ❌ EXCEPTION in {name}: {e}")
                import traceback
                traceback.print_exc()
                screenshot(page, f"error_{name.replace(' ', '_')}")

        browser.close()

    total = passed + failed
    print(f"\n{'='*60}")
    print(f"Results: {passed}/{total} passed, {failed} failed")
    print(f"Screenshots: {SCREENSHOT_DIR}/")
    if errors:
        print(f"\nFailed:")
        for name, detail in errors:
            print(f"  - {name}: {detail}")
    print(f"{'='*60}")

    return failed == 0


if __name__ == "__main__":
    ok = run_all()
    sys.exit(0 if ok else 1)
