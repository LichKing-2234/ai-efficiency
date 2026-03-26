"""
Playwright E2E test: verify that SSO (user role) and Dev Login (admin role)
see different content on the Settings page.

After fix:
- Dev login (admin): Settings page accessible, SCM Providers + LLM Config visible
- SSO user (user role): Settings link hidden in sidebar, /settings redirects to /

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


def do_dev_login(page):
    """Login via Dev Login button (admin role)."""
    page.goto(f"{BASE}/login")
    page.wait_for_load_state("networkidle")
    page.locator("button:has-text('Dev Login')").click()
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

    # Check SCM Providers section
    report("SCM Providers heading visible",
           page.locator("h1:has-text('SCM Providers')").is_visible())
    report("Add Provider button visible",
           page.locator("button:has-text('Add Provider')").is_visible())
    report("SCM table header: Name",
           page.locator("th:has-text('Name')").is_visible())

    # Check LLM Configuration section
    report("LLM Configuration heading visible",
           page.locator("h2:has-text('LLM Configuration')").is_visible())
    report("LLM Save button visible",
           page.locator("button:has-text('Save')").is_visible())
    report("LLM Test Connection button visible",
           page.locator("button:has-text('Test Connection')").is_visible())

    do_logout(page)


def test_user_role_settings_blocked(page):
    """
    Test: User role cannot access Settings page.
    - Settings link hidden in sidebar
    - /settings route redirects to /
    """
    print("\n🧪 User Role — Settings Page Blocked")

    # Dev login to get a valid token
    do_dev_login(page)

    # Intercept /auth/me to return role=user
    def handle_me(route):
        route.fulfill(
            status=200,
            content_type="application/json",
            body=json.dumps({
                "code": 0,
                "data": {
                    "user_id": 999,
                    "username": "sso_test_user",
                    "role": "user",
                }
            })
        )

    page.route("**/api/v1/auth/me", handle_me)

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
    page.unroute("**/api/v1/auth/me")
    do_logout(page)


def test_user_role_admin_ldap_blocked(page):
    """Test: User role cannot access /admin/ldap either."""
    print("\n🧪 User Role — /admin/ldap Blocked")

    do_dev_login(page)

    def handle_me(route):
        route.fulfill(
            status=200,
            content_type="application/json",
            body=json.dumps({
                "code": 0,
                "data": {
                    "user_id": 999,
                    "username": "sso_test_user",
                    "role": "user",
                }
            })
        )

    page.route("**/api/v1/auth/me", handle_me)
    page.goto(BASE)
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)

    # Navigate to /admin/ldap — should redirect
    page.goto(f"{BASE}/admin/ldap")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(1000)

    report("User role redirected away from /admin/ldap",
           "/admin/ldap" not in page.url,
           f"URL: {page.url}")

    page.unroute("**/api/v1/auth/me")
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
            ("User Role /admin/ldap Blocked", lambda: test_user_role_admin_ldap_blocked(page)),
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
