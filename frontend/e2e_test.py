"""
AI Efficiency Platform — Playwright E2E Tests
Covers Phase 1 (Login, Dashboard, Sidebar, Repos, Settings/SCM Providers)
and Phase 2 (Dashboard metrics, Repo detail scan/PR sections) frontend features.

All test-created data (SCM providers, repos) is cleaned up at the end to avoid
polluting the environment.

Usage:
  source .venv/bin/activate
  python scripts/with_server.py \
    --server "cd backend && go run ./cmd/server/" --port 8081 \
    --server "cd frontend && npm run dev -- --host 0.0.0.0" --port 5173 \
    -- python e2e_test.py
"""

import sys
import os
import traceback
from playwright.sync_api import sync_playwright, Page

BASE = "http://localhost:5173"
API = "http://localhost:8081/api/v1"
SCREENSHOT_DIR = "/tmp/ae-e2e"

passed = 0
failed = 0
errors = []

# Track created resources for cleanup
created_provider_ids = []
created_repo_ids = []
auth_token = None


def screenshot(page: Page, name: str):
    page.screenshot(path=f"{SCREENSHOT_DIR}/{name}.png", full_page=True)


def report(name: str, ok: bool, detail: str = ""):
    global passed, failed
    if ok:
        passed += 1
        print(f"  ✅ {name}")
    else:
        failed += 1
        errors.append((name, detail))
        print(f"  ❌ {name}: {detail}")


# ---------------------------------------------------------------------------
# Helper: dev login via API to get token (for cleanup calls)
# ---------------------------------------------------------------------------
def api_login(page: Page):
    """Get auth token via dev-login API for cleanup operations."""
    global auth_token
    import urllib.request, json
    req = urllib.request.Request(
        f"{API}/auth/dev-login",
        data=json.dumps({"username": "dev"}).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req) as resp:
            body = json.loads(resp.read())
            auth_token = body.get("data", {}).get("access_token")
    except Exception:
        # Fallback: extract from localStorage
        token = page.evaluate("localStorage.getItem('token')")
        auth_token = token


def api_delete(path: str):
    """DELETE via API with auth token."""
    import urllib.request
    req = urllib.request.Request(
        f"{API}{path}",
        method="DELETE",
        headers={"Authorization": f"Bearer {auth_token}"},
    )
    try:
        urllib.request.urlopen(req)
    except Exception as e:
        print(f"    ⚠️  Cleanup DELETE {path} failed: {e}")


# ---------------------------------------------------------------------------
# Phase 1 Tests
# ---------------------------------------------------------------------------

def test_login_page_renders(page: Page):
    """P1: Login page shows title, form fields, dev login button."""
    page.goto(f"{BASE}/login")
    page.wait_for_load_state("networkidle")
    screenshot(page, "01_login_page")

    report("Login title visible",
           page.locator("h1:has-text('AI Efficiency Platform')").is_visible())
    report("Username input visible",
           page.locator("#username").is_visible())
    report("Password input visible",
           page.locator("#password").is_visible())
    report("Auth source selector visible",
           page.locator("#source").is_visible())
    report("SSO option exists",
           page.locator("option[value='SSO']").count() == 1)
    report("LDAP option exists",
           page.locator("option[value='LDAP']").count() == 1)
    report("Sign in button visible",
           page.locator("button[type='submit']:has-text('Sign in')").is_visible())
    report("Dev Login button visible",
           page.locator("button:has-text('Dev Login')").is_visible())


def test_unauthenticated_redirect(page: Page):
    """P1: Protected routes redirect to /login."""
    for path in ["/", "/repos", "/settings"]:
        page.goto(f"{BASE}{path}")
        page.wait_for_load_state("networkidle")
        report(f"Redirect {path} -> /login",
               "/login" in page.url)


def test_dev_login(page: Page):
    """P1: Dev login flow — click Dev Login, land on dashboard."""
    page.goto(f"{BASE}/login")
    page.wait_for_load_state("networkidle")
    page.locator("button:has-text('Dev Login')").click()
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(1000)
    screenshot(page, "02_after_dev_login")
    report("Dev login redirects to dashboard",
           page.url.rstrip("/") == BASE or page.url == f"{BASE}/")
    # Grab token for cleanup
    api_login(page)


def test_dashboard_page(page: Page):
    """P1+P2: Dashboard shows welcome, metric cards."""
    page.goto(BASE)
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)
    screenshot(page, "03_dashboard")

    report("Dashboard welcome text",
           page.locator("h1:has-text('Welcome back')").is_visible())
    report("Dashboard card: Total Repos",
           page.locator("text=Total Repos").is_visible())
    report("Dashboard card: Active Sessions",
           page.locator("text=Active Sessions").is_visible())
    report("Dashboard card: Avg AI Score",
           page.locator("text=Avg AI Score").is_visible())
    report("Dashboard card: AI PRs",
           page.locator("text=AI PRs").is_visible())
    report("Dashboard Recent Activity section",
           page.locator("h2:has-text('Recent Activity')").is_visible())


def test_sidebar_navigation(page: Page):
    """P1: Sidebar links work — Dashboard, Repos, Settings."""
    page.goto(BASE)
    page.wait_for_load_state("networkidle")

    sidebar = page.locator("aside")
    report("Sidebar visible", sidebar.is_visible())
    report("Sidebar brand text",
           sidebar.locator("text=AI Efficiency").first.is_visible())
    report("Sidebar Dashboard link",
           sidebar.locator("a:has-text('Dashboard')").is_visible())
    report("Sidebar Repos link",
           sidebar.locator("a:has-text('Repos')").is_visible())
    report("Sidebar Settings link",
           sidebar.locator("a:has-text('Settings')").is_visible())
    report("Sidebar Analysis disabled",
           sidebar.locator("span:has-text('Analysis')").is_visible())
    report("Sidebar Gating disabled",
           sidebar.locator("span:has-text('Gating')").is_visible())

    sidebar.locator("a:has-text('Repos')").click()
    page.wait_for_url("**/repos**")
    page.wait_for_load_state("networkidle")
    report("Sidebar nav to /repos", "/repos" in page.url)

    sidebar.locator("a:has-text('Settings')").click()
    page.wait_for_url("**/settings**")
    page.wait_for_load_state("networkidle")
    report("Sidebar nav to /settings", "/settings" in page.url)

    sidebar.locator("a:has-text('Dashboard')").click()
    page.wait_for_load_state("networkidle")
    report("Sidebar nav back to /",
           page.url.rstrip("/") == BASE or page.url == f"{BASE}/")


def test_sidebar_user_info(page: Page):
    """P1: Sidebar shows user info and logout button."""
    page.goto(BASE)
    page.wait_for_load_state("networkidle")
    sidebar = page.locator("aside")
    report("Sidebar user section visible",
           sidebar.locator("button[title='Logout']").is_visible())


def test_settings_scm_providers_page(page: Page):
    """P1: Settings page shows SCM Providers heading and table."""
    page.goto(f"{BASE}/settings")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)
    screenshot(page, "04_settings")

    report("Settings heading: SCM Providers",
           page.locator("h1:has-text('SCM Providers')").is_visible())
    report("Add Provider button visible",
           page.locator("button:has-text('Add Provider')").is_visible())
    for col in ["Name", "Type", "Base URL", "Status", "Created", "Actions"]:
        report(f"Settings table header: {col}",
               page.locator(f"th:has-text('{col}')").is_visible())


def test_add_scm_provider_dialog(page: Page):
    """P1: Add Provider dialog opens with correct form fields."""
    page.goto(f"{BASE}/settings")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)

    page.locator("button:has-text('Add Provider')").click()
    page.wait_for_timeout(300)
    screenshot(page, "05_add_provider_dialog")

    dialog = page.locator(".fixed")
    report("Add Provider dialog visible", dialog.is_visible())
    report("Dialog title: Add SCM Provider",
           dialog.locator("h2:has-text('Add SCM Provider')").is_visible())
    report("Dialog Name input",
           dialog.locator("input[placeholder*='GitHub']").is_visible())
    report("Dialog Type selector (github option)",
           dialog.locator("option[value='github']").count() >= 1)
    report("Dialog Type selector (bitbucket option)",
           dialog.locator("option[value='bitbucket_server']").count() >= 1)
    report("Dialog Base URL input",
           dialog.locator("input[placeholder*='api.github.com']").is_visible())
    report("Dialog Token input",
           dialog.locator("input[type='password']").is_visible())
    report("Dialog Create button",
           dialog.locator("button:has-text('Create')").is_visible())
    report("Dialog Cancel button",
           dialog.locator("button:has-text('Cancel')").is_visible())

    dialog.locator("button:has-text('Cancel')").click()
    page.wait_for_timeout(300)
    report("Cancel closes dialog", page.locator(".fixed").count() == 0)


def test_create_scm_provider(page: Page):
    """P1: Create a GitHub SCM provider via the dialog."""
    page.goto(f"{BASE}/settings")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)

    # Count existing providers
    before_count = page.locator("tbody tr").count()

    page.locator("button:has-text('Add Provider')").click()
    page.wait_for_timeout(300)

    dialog = page.locator(".fixed")
    dialog.locator("input[placeholder*='GitHub']").fill("E2E-Test-Provider")
    dialog.locator("input[type='password']").fill("ghp_e2e_test_token")
    dialog.locator("button:has-text('Create')").click()
    page.wait_for_timeout(1500)
    screenshot(page, "06_after_create_provider")

    dialog_gone = page.locator(".fixed").count() == 0
    has_provider = page.locator("td:has-text('E2E-Test-Provider')").count() > 0
    report("SCM provider created", dialog_gone and has_provider)

    # Track for cleanup — extract ID from the API
    if has_provider:
        import urllib.request, json
        req = urllib.request.Request(
            f"{API}/scm-providers",
            headers={"Authorization": f"Bearer {auth_token}"},
        )
        try:
            with urllib.request.urlopen(req) as resp:
                body = json.loads(resp.read())
                items = body.get("data", [])
                if isinstance(items, dict):
                    items = items.get("items", [])
                for p in items:
                    if p.get("name") == "E2E-Test-Provider":
                        created_provider_ids.append(p["id"])
        except Exception:
            pass


def test_edit_scm_provider(page: Page):
    """P1: Edit an existing SCM provider."""
    page.goto(f"{BASE}/settings")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)

    edit_btn = page.locator("tr:has-text('E2E-Test-Provider') button:has-text('Edit')")
    if edit_btn.count() > 0:
        edit_btn.click()
        page.wait_for_timeout(300)

        dialog = page.locator(".fixed")
        report("Edit dialog title: Edit Provider",
               dialog.locator("h2:has-text('Edit Provider')").is_visible())
        report("Edit dialog Update button",
               dialog.locator("button:has-text('Update')").is_visible())

        # Change name
        name_input = dialog.locator("input[placeholder*='GitHub']")
        name_input.fill("E2E-Test-Provider-Edited")
        dialog.locator("button:has-text('Update')").click()
        page.wait_for_timeout(1500)

        has_edited = page.locator("td:has-text('E2E-Test-Provider-Edited')").count() > 0
        report("SCM provider edited", has_edited)

        # Rename back for later cleanup matching
        if has_edited:
            edit_btn2 = page.locator("tr:has-text('E2E-Test-Provider-Edited') button:has-text('Edit')")
            if edit_btn2.count() > 0:
                edit_btn2.click()
                page.wait_for_timeout(300)
                dialog2 = page.locator(".fixed")
                dialog2.locator("input[placeholder*='GitHub']").fill("E2E-Test-Provider")
                dialog2.locator("button:has-text('Update')").click()
                page.wait_for_timeout(1000)
    else:
        report("Edit SCM provider: SKIPPED (provider not found)", True)


def test_repos_page(page: Page):
    """P1: Repos page shows heading, Add Repo button, table."""
    page.goto(f"{BASE}/repos")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)
    screenshot(page, "07_repos_page")

    report("Repos heading",
           page.locator("h1:has-text('Repositories')").is_visible())
    report("Add Repo button",
           page.locator("button:has-text('Add Repo')").is_visible())
    for col in ["Name", "AI Score", "Status", "Last Scan", "Actions"]:
        report(f"Repos table header: {col}",
               page.locator(f"th:has-text('{col}')").is_visible())


def test_add_repo_dialog(page: Page):
    """P1: Add Repo dialog opens with correct form fields."""
    page.goto(f"{BASE}/repos")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)

    page.locator("button:has-text('Add Repo')").click()
    page.wait_for_timeout(500)
    screenshot(page, "08_add_repo_dialog")

    dialog = page.locator(".fixed")
    report("Add Repo dialog visible", dialog.is_visible())
    report("Dialog title: Add Repository",
           dialog.locator("h2:has-text('Add Repository')").is_visible())
    report("Dialog SCM Provider selector",
           dialog.locator("select").first.is_visible())
    report("Dialog Repo URL input",
           dialog.locator("input[placeholder*='github.com']").is_visible())
    report("Dialog Default Branch input",
           dialog.locator("input[type='text']").last.is_visible())
    report("Dialog Add button",
           dialog.locator("button:has-text('Add')").is_visible())

    dialog.locator("button:has-text('Cancel')").click()
    page.wait_for_timeout(300)
    report("Add Repo Cancel closes dialog", page.locator(".fixed").count() == 0)


def test_add_repo_url_parsing(page: Page):
    """P1: Repo URL auto-parsing for GitHub and Bitbucket URLs."""
    page.goto(f"{BASE}/repos")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)

    page.locator("button:has-text('Add Repo')").click()
    page.wait_for_timeout(300)

    dialog = page.locator(".fixed")
    url_input = dialog.locator("input[placeholder*='github.com']")

    url_input.fill("https://github.com/octocat/hello-world")
    url_input.dispatch_event("input")
    page.wait_for_timeout(300)
    screenshot(page, "09_repo_url_parsed_github")

    report("GitHub URL parsed: full_name shown",
           dialog.locator("text=octocat/hello-world").is_visible())
    report("GitHub URL parsed: name shown",
           dialog.locator("text=hello-world").count() > 0)

    http_btn = dialog.locator("button:has-text('HTTP')")
    ssh_btn = dialog.locator("button:has-text('SSH')")
    report("HTTP/SSH toggle visible",
           http_btn.is_visible() and ssh_btn.is_visible())

    url_input.fill("https://bitbucket.example.com/projects/PROJ/repos/my-repo/browse")
    url_input.dispatch_event("input")
    page.wait_for_timeout(300)
    screenshot(page, "10_repo_url_parsed_bitbucket")

    report("Bitbucket URL parsed: full_name shown",
           dialog.locator("text=PROJ/my-repo").is_visible())

    dialog.locator("button:has-text('Cancel')").click()


def test_create_repo(page: Page):
    """P1: Create a repo via the Add Repo dialog."""
    page.goto(f"{BASE}/repos")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)

    page.locator("button:has-text('Add Repo')").click()
    page.wait_for_timeout(300)

    dialog = page.locator(".fixed")
    url_input = dialog.locator("input[placeholder*='github.com']")
    url_input.fill("https://github.com/e2e-test-org/e2e-test-repo")
    url_input.dispatch_event("input")
    page.wait_for_timeout(300)

    dialog.locator("button:has-text('Add')").click()
    page.wait_for_timeout(1500)
    screenshot(page, "11_after_create_repo")

    dialog_gone = page.locator(".fixed").count() == 0
    has_repo = page.locator("td:has-text('e2e-test-repo')").count() > 0
    report("Repo created (dialog closed and repo in table)",
           dialog_gone and has_repo)

    # Track for cleanup
    if has_repo:
        import urllib.request, json
        req = urllib.request.Request(
            f"{API}/repos",
            headers={"Authorization": f"Bearer {auth_token}"},
        )
        try:
            with urllib.request.urlopen(req) as resp:
                body = json.loads(resp.read())
                data = body.get("data", {})
                items = data.get("items", []) if isinstance(data, dict) else data
                for r in items:
                    if r.get("full_name") == "e2e-test-org/e2e-test-repo":
                        created_repo_ids.append(r["id"])
        except Exception:
            pass


def test_repo_detail_page(page: Page):
    """P1+P2: Repo detail page shows info, scan section, PR section."""
    page.goto(f"{BASE}/repos")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)

    row = page.locator("tr:has-text('e2e-test-repo')")
    if row.count() > 0:
        row.first.click()
        page.wait_for_load_state("networkidle")
        page.wait_for_timeout(500)
        screenshot(page, "12_repo_detail")

        report("Repo detail: Back to Repos link",
               page.locator("button:has-text('Back to Repos')").is_visible())
        report("Repo detail: repo name heading",
               page.locator("h1").first.is_visible())
        report("Repo detail: Run Scan button",
               page.locator("button:has-text('Run Scan')").is_visible())
        report("Repo detail: Sync PRs button",
               page.locator("button:has-text('Sync PRs')").is_visible())
        report("Repo detail: AI Score badge",
               page.locator("text=AI Score").is_visible())
        report("Repo detail: Basic Info section",
               page.locator("h2:has-text('Basic Info')").is_visible())
        for field in ["Clone URL", "Default Branch", "Status", "Last Scan", "Created"]:
            report(f"Repo detail info: {field}",
                   page.locator(f"dt:has-text('{field}')").is_visible())
        report("Repo detail: Score Breakdown section",
               page.locator("h2:has-text('Score Breakdown')").is_visible())
        report("Repo detail: Scan History section",
               page.locator("h2:has-text('Scan History')").is_visible())
        report("Repo detail: Pull Requests section",
               page.locator("h2:has-text('Pull Requests')").is_visible())

        page.locator("button:has-text('Back to Repos')").click()
        page.wait_for_load_state("networkidle")
        report("Back to Repos navigates to /repos", "/repos" in page.url)
    else:
        report("Repo detail: SKIPPED (no repos in table)", True)
        print("    ⚠️  No repos found — repo detail tests skipped")


def test_repo_delete_confirm_cancel(page: Page):
    """P1: Repo delete shows confirmation, cancel restores state."""
    page.goto(f"{BASE}/repos")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)

    delete_btn = page.locator("tr:has-text('e2e-test-repo') button:has-text('Delete')")
    if delete_btn.count() > 0:
        delete_btn.click()
        page.wait_for_timeout(300)
        screenshot(page, "13_repo_delete_confirm")

        report("Delete confirm: Confirm button appears",
               page.locator("button:has-text('Confirm')").is_visible())
        report("Delete confirm: Cancel button appears",
               page.locator("button.text-gray-500:has-text('Cancel')").is_visible())

        page.locator("button.text-gray-500:has-text('Cancel')").first.click()
        page.wait_for_timeout(300)
        report("Delete cancel restores Delete button",
               page.locator("tr:has-text('e2e-test-repo') button:has-text('Delete')").count() > 0)
    else:
        report("Repo delete confirm: SKIPPED (no repos)", True)


def test_repo_delete_execute(page: Page):
    """P1: Actually delete the test repo via UI."""
    page.goto(f"{BASE}/repos")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)

    delete_btn = page.locator("tr:has-text('e2e-test-repo') button:has-text('Delete')")
    if delete_btn.count() > 0:
        delete_btn.click()
        page.wait_for_timeout(300)
        page.locator("tr:has-text('e2e-test-repo') button:has-text('Confirm')").click()
        page.wait_for_timeout(1500)
        screenshot(page, "14_after_repo_delete")

        repo_gone = page.locator("td:has-text('e2e-test-repo')").count() == 0
        report("Repo deleted from table", repo_gone)
        if repo_gone:
            # Already cleaned up via UI, remove from cleanup list
            created_repo_ids.clear()
    else:
        report("Repo delete execute: SKIPPED (no repos)", True)


def test_scm_provider_delete(page: Page):
    """P1: Delete the test SCM provider via UI."""
    page.goto(f"{BASE}/settings")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)

    delete_btn = page.locator("tr:has-text('E2E-Test-Provider') button:has-text('Delete')")
    if delete_btn.count() > 0:
        delete_btn.click()
        page.wait_for_timeout(300)
        screenshot(page, "15_provider_delete_confirm")

        report("Provider delete: Confirm button appears",
               page.locator("tr:has-text('E2E-Test-Provider') button:has-text('Confirm')").is_visible())

        page.locator("tr:has-text('E2E-Test-Provider') button:has-text('Confirm')").click()
        page.wait_for_timeout(1500)
        screenshot(page, "16_after_provider_delete")

        provider_gone = page.locator("td:has-text('E2E-Test-Provider')").count() == 0
        report("SCM provider deleted from table", provider_gone)
        if provider_gone:
            created_provider_ids.clear()
    else:
        report("Provider delete: SKIPPED (provider not found)", True)


def test_logout(page: Page):
    """P1: Logout clears auth and redirects to login."""
    page.goto(BASE)
    page.wait_for_load_state("networkidle")

    page.locator("button[title='Logout']").click()
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(500)
    screenshot(page, "17_after_logout")

    report("Logout redirects to /login", "/login" in page.url)


# ---------------------------------------------------------------------------
# Cleanup: delete any leftover test data via API
# ---------------------------------------------------------------------------
def cleanup():
    """Delete any test data that wasn't cleaned up by the UI tests."""
    print("\n🧹 Cleanup")
    if not auth_token:
        print("  ⚠️  No auth token — skipping API cleanup")
        return

    for rid in created_repo_ids:
        print(f"  Deleting repo id={rid}")
        api_delete(f"/repos/{rid}")

    for pid in created_provider_ids:
        print(f"  Deleting provider id={pid}")
        api_delete(f"/scm-providers/{pid}")

    if not created_repo_ids and not created_provider_ids:
        print("  ✅ Nothing to clean up — all test data removed by UI tests")


# ---------------------------------------------------------------------------
# Runner
# ---------------------------------------------------------------------------
def run_all():
    global passed, failed, errors

    os.makedirs(SCREENSHOT_DIR, exist_ok=True)

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1280, "height": 800})
        page = context.new_page()

        console_errors = []
        page.on("console", lambda msg: console_errors.append(msg.text) if msg.type == "error" else None)

        tests = [
            # --- Auth & Navigation ---
            ("Phase 1: Login Page Rendering", test_login_page_renders),
            ("Phase 1: Unauthenticated Redirect", test_unauthenticated_redirect),
            ("Phase 1: Dev Login Flow", test_dev_login),
            ("Phase 1+2: Dashboard Page", test_dashboard_page),
            ("Phase 1: Sidebar Navigation", test_sidebar_navigation),
            ("Phase 1: Sidebar User Info", test_sidebar_user_info),
            # --- SCM Providers CRUD ---
            ("Phase 1: Settings / SCM Providers Page", test_settings_scm_providers_page),
            ("Phase 1: Add SCM Provider Dialog", test_add_scm_provider_dialog),
            ("Phase 1: Create SCM Provider", test_create_scm_provider),
            ("Phase 1: Edit SCM Provider", test_edit_scm_provider),
            # --- Repos CRUD ---
            ("Phase 1: Repos Page", test_repos_page),
            ("Phase 1: Add Repo Dialog", test_add_repo_dialog),
            ("Phase 1: Add Repo URL Parsing", test_add_repo_url_parsing),
            ("Phase 1: Create Repo", test_create_repo),
            ("Phase 1+2: Repo Detail Page", test_repo_detail_page),
            ("Phase 1: Repo Delete Confirmation (Cancel)", test_repo_delete_confirm_cancel),
            # --- Delete tests (cleanup via UI) ---
            ("Phase 1: Repo Delete (Execute)", test_repo_delete_execute),
            ("Phase 1: SCM Provider Delete", test_scm_provider_delete),
            # --- Logout ---
            ("Phase 1: Logout", test_logout),
        ]

        for name, fn in tests:
            print(f"\n🧪 {name}")
            try:
                fn(page)
            except Exception as e:
                failed += 1
                detail = f"EXCEPTION: {e}"
                errors.append((name, detail))
                print(f"  ❌ {name}: {detail}")
                traceback.print_exc()
                screenshot(page, f"error_{name.replace(' ', '_')}")

        browser.close()

    # Always run cleanup as safety net
    cleanup()

    # Summary
    total = passed + failed
    print(f"\n{'='*60}")
    print(f"Results: {passed}/{total} passed, {failed} failed")
    print(f"Screenshots saved to {SCREENSHOT_DIR}/")
    if errors:
        print(f"\nFailed checks:")
        for name, detail in errors:
            print(f"  - {name}: {detail}")
    print(f"{'='*60}")

    return failed == 0


if __name__ == "__main__":
    ok = run_all()
    sys.exit(0 if ok else 1)
