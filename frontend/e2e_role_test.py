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
    page.route("**/api/v1/attribution/report**", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({
            "code": 0,
            "data": {
                "from": "2026-08-01T00:00:00Z",
                "to": "2026-08-08T00:00:00Z",
                "measured_tokens": 190000,
                "bound_tokens": 162000,
                "unbound_tokens": 28000,
                "shared_tokens": 16000,
                "historical_advisory_tokens": 0,
                "allocation_rate": 0.8526,
                "coverage_gap_count": 2,
                "request_id_coverage_count": 2,
                "bucket_count": 4,
                "evidence": {
                    "measured_buckets": 4,
                    "historical_advisory_buckets": 0,
                    "invalid_buckets": 0,
                    "exact_correlation_buckets": 0,
                    "advisory_correlation_buckets": 2,
                    "unlinked_correlation_buckets": 2,
                },
                "repositories": [
                    {
                        "repo_config_id": 0,
                        "repo_key": "unbound",
                        "name": "Unbound",
                        "tokens": 0,
                        "processed_tokens": 12000,
                        "unbound_tokens": 12000,
                        "shared_tokens": 0,
                        "inherited_tokens": 0,
                        "worktrees": None,
                        "branches": None,
                        "commits": None,
                    },
                    {
                        "repo_config_id": 7,
                        "repo_key": "github.com/example-org/ai-efficiency",
                        "name": "example-org/ai-efficiency",
                        "tokens": 162000,
                        "processed_tokens": 162000,
                        "unbound_tokens": 0,
                        "shared_tokens": 16000,
                        "inherited_tokens": 0,
                        "worktrees": ["worktree/ai-efficiency"],
                        "branches": ["codex/poc-token-attribution"],
                        "commits": [{
                            "commit_sha": "3f4a9d9f25c582ea38b7347c7498692fb01022ab",
                            "lineage": "",
                            "tokens": 162000,
                            "inherited_tokens": 0,
                            "inherited_from_commit_shas": [],
                            "prs": [{
                                "id": 42,
                                "scm_pr_id": 42,
                                "title": "Compact Codex attribution",
                                "url": "https://example.com/pull/42",
                                "status": "open",
                            }],
                        }],
                    },
                ],
                "buckets": [],
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
    page.wait_for_timeout(300)
    report("Code Platforms section visible",
           page.locator("h2:has-text('Code Platforms')").is_visible())
    report("Add Platform button visible",
           page.locator("button:has-text('Add Platform')").is_visible())

    page.locator("[data-testid='settings-tab-deployment-runtime']").click()
    page.wait_for_timeout(300)
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


def test_attribution_route_layout_and_responsive_style(page):
    """Protected attribution returns after login and renders inside the shared shell."""
    print("\n🧪 Attribution — Route, Layout, and Responsive Style")

    mock_auth_endpoints(page, role="admin")
    page.goto(f"{BASE}/login")
    page.evaluate("localStorage.clear()")

    page_errors = []
    on_page_error = lambda error: page_errors.append(str(error))
    page.on("pageerror", on_page_error)

    page.goto(f"{BASE}/attribution")
    page.wait_for_load_state("networkidle")
    report("Protected attribution preserves the requested redirect",
           page.url == f"{BASE}/login?redirect=/attribution",
           f"URL: {page.url}")

    page.locator("button:has-text('Dev Login')").click()
    page.wait_for_timeout(600)
    report("Dev login returns to /attribution",
           page.url == f"{BASE}/attribution",
           f"URL: {page.url}")

    if page.url != f"{BASE}/attribution":
        page.goto(f"{BASE}/attribution")
    page.wait_for_timeout(800)

    sidebar = page.locator("aside")
    attribution_link = page.locator("aside a[href='/attribution']")
    report("Attribution uses the shared desktop app shell",
           sidebar.count() == 1 and sidebar.is_visible(),
           f"aside count: {sidebar.count()}")
    report("Attribution navigation is active",
           attribution_link.count() == 1 and "bg-gray-800" in (attribution_link.get_attribute("class") or ""),
           f"class: {attribution_link.get_attribute('class') if attribution_link.count() else None}")
    report("Attribution ledger renders compact API data",
           page.locator("[data-testid='attribution-measured']").count() == 1
           and page.locator("[data-testid='attribution-measured']").inner_text() == "190,000")
    report("Nullable compact collections do not crash the page",
           not page_errors,
           repr(page_errors))
    report("Attribution page has no desktop horizontal overflow",
           page.evaluate("document.documentElement.scrollWidth <= window.innerWidth"))
    report("Attribution refresh uses the platform primary color",
           page.locator("[data-testid='attribution-refresh']").count() == 1
           and page.locator("[data-testid='attribution-refresh']").evaluate(
               "element => getComputedStyle(element).backgroundColor"
           ) == "rgb(14, 116, 144)")
    screenshot(page, "05_attribution_desktop")

    page.set_viewport_size({"width": 390, "height": 844})
    page.reload()
    page.wait_for_timeout(800)
    report("Attribution uses the shared mobile header",
           page.locator("header button:has-text('Menu')").is_visible())
    report("Attribution page has no mobile horizontal overflow",
           page.evaluate("document.documentElement.scrollWidth <= window.innerWidth"))
    screenshot(page, "06_attribution_mobile")

    page.remove_listener("pageerror", on_page_error)
    page.set_viewport_size({"width": 1440, "height": 900})
    page.evaluate("localStorage.clear()")
    clear_auth_routes(page)


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
            ("Attribution Route and Layout", lambda: test_attribution_route_layout_and_responsive_style(page)),
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
