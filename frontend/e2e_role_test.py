"""
Playwright E2E test: verify that SSO (user role) and Dev Login (admin role)
see different content on the Settings page.

After task-zone frontend refactor:
- Dev login (admin): Settings page accessible, Admin Console task zones visible
- SSO user (user role): Admin links hidden in sidebar, /settings and /admin/users redirect to /

Usage:
  python frontend/e2e_role_test.py
"""

import sys
import os
import json
from playwright.sync_api import sync_playwright

BASE = "http://localhost:5173"
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
        "**/api/v1/events**",
        "**/api/v1/scm-providers**",
        "**/api/v1/admin/providers**",
        "**/api/v1/admin/credentials**",
        "**/api/v1/settings/deployment**",
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
    page.route("**/api/v1/settings/deployment**", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({
            "code": 0,
            "data": {
                "version": {"version": "v0.0.0-test", "commit": "test", "build_time": "2026-01-01T00:00:00Z"},
                "mode": "test",
                "update_available": False,
                "update_status": {"phase": "idle"},
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
    report("Restart Service button visible",
           page.locator("button:has-text('Restart Service')").is_visible())

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


def run_all():
    global passed, failed

    os.makedirs(SCREENSHOT_DIR, exist_ok=True)

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1280, "height": 800})
        page = context.new_page()

        tests = [
            ("Admin (Dev Login) Settings", lambda: test_dev_login_settings(page)),
            ("User Role Settings Blocked", lambda: test_user_role_settings_blocked(page)),
            ("User Role /admin/users Blocked", lambda: test_user_role_admin_users_blocked(page)),
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
