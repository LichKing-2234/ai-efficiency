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

VIEWPORTS = (
    ("mobile", 390, 844),
    ("tablet", 768, 1024),
    ("desktop", 1440, 900),
)

USER_ROUTE_CASES = (
    {"path": "/usage", "selector": "[data-testid='usage-center-tabs']"},
    {"path": "/usage/team", "selector": "[data-testid='team-overview-content']"},
    {"path": "/usage/members/7", "selector": "[data-testid='member-usage-back']"},
    {"path": "/usage/quota-reset", "selector": "[data-testid='quota-reset-queue-selector']"},
    {"path": "/work-items", "selector": "main h1:has-text('Work Items')"},
    {"path": "/repos", "selector": "[data-testid='repo-binding-filter']", "exercise": "repo-dialog"},
    {"path": "/repos/9", "selector": "[data-testid='repo-tab-activity']"},
    {"path": "/activity", "selector": "[data-testid='activity-range-refresh']"},
    {"path": "/activity/teams", "selector": "[data-testid='activity-team-team-alpha']"},
    {"path": "/activity/teams/team-alpha", "selector": "[data-testid='activity-team-summary']"},
    {"path": "/activity/members/7", "selector": "[data-testid='activity-range-refresh']"},
    {"path": "/user", "selector": "main button:has-text('Refresh')"},
)

ADMIN_ROUTE_CASES = (
    {"path": "/admin/users", "selector": "[data-testid='admin-users-view-users']"},
    {
        "path": "/admin/directory/offboarding",
        "selector": "[data-testid='offboarding-search']",
        "exercise": "offboarding-dialog",
    },
    {
        "path": "/settings",
        "selector": "[data-testid='settings-section-select']",
        "desktop_selector": "[data-testid='settings-tab-ai-services']",
        "exercise": "settings-dialog",
    },
)

PUBLIC_ROUTE_CASES = (
    {"path": "/login", "selector": "[data-testid='username-field']", "authenticated": False},
    {
        "path": "/oauth/authorize?client_id=ae-cli&redirect_uri=http%3A%2F%2F127.0.0.1%2Fcallback&state=e2e-state",
        "expected_path": "/oauth/authorize",
        "selector": "a[href^='/login?redirect=']",
        "authenticated": False,
    },
    {"path": "/oauth/device", "selector": "[data-testid='device-code']", "authenticated": True},
)


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


def fulfill_json(route, data, status=200):
    route.fulfill(
        status=status,
        content_type="application/json",
        body=json.dumps({"code": 0 if status < 400 else status, "data": data} if status < 400 else {
            "code": status,
            "message": data.get("message", "Mock request failed"),
        }),
    )


def activity_metrics():
    return {
        "participating_prs": {"value": 2, "lower_bound": False},
        "merged_prs": {"value": 1, "lower_bound": False},
        "active_repositories": 1,
        "commit_count": 1,
        "latest_activity": "2026-08-05T12:00:00Z",
    }


def activity_sync_coverage():
    return {
        "complete": True,
        "affected_repositories": 0,
        "unsynced_repositories": 0,
        "stale_repositories": 0,
        "partially_synced_repositories": 0,
        "failed_repositories": 0,
    }


def activity_member_row():
    return {
        "member": {
            "user_id": 7,
            "display_name": "Alice",
            "email": "alice@example.com",
            "department_external_ids": ["team-alpha"],
        },
        "available": True,
        "metrics": activity_metrics(),
        "quality": {
            "measured_buckets": 1,
            "unbound_buckets": 0,
            "multi_repo_shared_buckets": 0,
            "invalid_token_facts": 0,
            "historical_advisory_facts": 0,
            "coverage_gap_count": 0,
        },
    }


def mock_matrix_api(route, role):
    path = urlparse(route.request.url).path
    repo = {
        "id": 9,
        "repo_key": "github.com/example-org/repo-a",
        "name": "repo-a",
        "full_name": "example-org/repo-a",
        "clone_url": "https://github.com/example-org/repo-a.git",
        "default_branch": "main",
        "status": "active",
        "binding_state": "bound",
        "group_id": 1,
        "scm_provider_id": 3,
        "created_at": "2026-08-01T00:00:00Z",
        "edges": {},
    }
    team_identity = {
        "external_id": "team-alpha",
        "parent_external_id": None,
        "name": "Team Alpha",
        "display_path": "Team Alpha",
        "member_count": 1,
    }

    responses = {
        "/api/v1/user/team-usage/scope": {
            "is_representative": True,
            "departments": [{
                "external_id": "team-alpha",
                "name": "Team Alpha",
                "display_path": "Team Alpha",
                "subtree_member_count": 1,
                "matched_user_count": 1,
            }],
        },
        "/api/v1/user/quota-reset/requests": {"items": [], "page": 1, "page_size": 20, "total": 0},
        "/api/v1/repos": {"items": [repo], "total": 1, "page": 1, "page_size": 20},
        "/api/v1/repos/inventory": [{
            "provider_key": "scm_provider:3",
            "provider_id": 3,
            "name": "GitHub",
            "type": "github",
            "total_repos": 1,
            "bound_repos": 1,
            "unbound_repos": 0,
            "active_repos": 1,
            "webhook_failed_repos": 0,
            "scopes": [{
                "scope": "example-org",
                "total_repos": 1,
                "bound_repos": 1,
                "unbound_repos": 0,
                "active_repos": 1,
                "webhook_failed_repos": 0,
            }],
        }],
        "/api/v1/repos/9": repo,
        "/api/v1/activity/scope": {
            "contract_version": "activity-v1",
            "scope_version": "scope-e2e",
            "can_view_teams": True,
            "admin": role == "admin",
            "representative": True,
            "teams": [team_identity],
        },
        "/api/v1/activity/teams/team-alpha": {
            "contract_version": "activity-v1",
            "scope_version": "scope-e2e",
            "window": {"from": "2026-07-09T00:00:00Z", "to": "2026-08-08T00:00:00Z"},
            "team": team_identity,
            "active_members": 1,
            "metrics": activity_metrics(),
            "sync_coverage": activity_sync_coverage(),
            "members": {"items": [activity_member_row()]},
        },
        "/api/v1/activity/repos/9": {
            "contract_version": "activity-v1",
            "scope_version": "scope-e2e",
            "window": {"from": "2026-07-09T00:00:00Z", "to": "2026-08-08T00:00:00Z"},
            "repository": {"repo_config_id": 9, "name": "example-org/repo-a"},
            "participating_members": 1,
            "metrics": activity_metrics(),
            "sync_coverage": activity_sync_coverage(),
            "members": {"items": [activity_member_row()]},
            "prs": {"items": []},
            "commits": {"items": []},
        },
        "/api/v1/admin/users": {"items": [], "total": 0, "page": 1, "page_size": 20},
        "/api/v1/admin/users/department-options": {
            "items": [], "selected": None, "total": 0, "page": 1, "page_size": 20,
        },
        "/api/v1/admin/users/subscription-options": {"providers": []},
        "/api/v1/admin/users/subscription-jobs/latest": None,
        "/api/v1/admin/directory/offboarding-candidates": {
            "items": [{
                "user_id": 7,
                "username": "bob",
                "email": "bob@example.org",
                "auth_source": "ldap",
                "relay_user_id": 97,
                "reason": "missing_from_latest_full_company_directory",
                "directory_run_id": 3,
            }],
            "page": 1,
            "page_size": 20,
            "total": 1,
        },
    }

    if path in responses:
        fulfill_json(route, responses[path])
        return
    fulfill_json(route, {"message": f"No E2E mock for {path}"}, status=503)


def clear_auth_routes(page):
    for pattern in [
        "**/api/v1/**",
        "**/api/v1/auth/options",
        "**/api/v1/auth/dev-login",
        "**/api/v1/auth/refresh",
        "**/api/v1/auth/me",
        "**/api/v1/efficiency/dashboard",
        "**/api/v1/user/providers",
        "**/api/v1/work-items/counts",
        "**/api/v1/attribution/report**",
        "**/api/v1/activity/summary**",
        "**/api/v1/activity/members/**",
        "**/api/v1/activity/buckets/**",
        "**/api/v1/scm-providers**",
        "**/api/v1/admin/providers**",
        "**/api/v1/admin/credentials**",
        "**/api/v1/system/version**",
        "**/api/v1/admin/settings/ldap**",
        "**/oauth/authorize/approve",
        "**/oauth/device/verify",
    ]:
        try:
            page.unroute(pattern)
        except Exception:
            pass


def mock_auth_endpoints(page, role="admin"):
    clear_auth_routes(page)

    page.route("**/api/v1/**", lambda route: mock_matrix_api(route, role))
    page.route("**/oauth/authorize/approve", lambda route: fulfill_json(
        route,
        {"redirect_uri": "http://127.0.0.1/callback?code=e2e-code"},
    ))
    page.route("**/oauth/device/verify", lambda route: fulfill_json(route, {"status": "approved"}))

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


def overflow_state(page):
    return page.evaluate("""() => ({
        viewport: window.innerWidth,
        documentWidth: document.documentElement.scrollWidth,
        bodyWidth: document.body.scrollWidth,
    })""")


def expected_selector(case, width):
    if width >= 1280 and case.get("desktop_selector"):
        return case["desktop_selector"]
    return case["selector"]


def protected_shell_state(page, width):
    menu = page.locator("header button:has-text('Menu')")
    sidebar = page.locator("aside").first
    if width < 768:
        return menu.is_visible() and not sidebar.is_visible()
    return sidebar.is_visible() and not menu.is_visible()


def exercise_route_control(page, exercise):
    if exercise == "repo-dialog":
        page.locator("main button:has-text('Add Repo')").click()
        dialog = page.locator(".el-dialog").first
        dialog.wait_for(state="visible")
        opened = dialog.is_visible()
        page.keyboard.press("Escape")
        dialog.wait_for(state="hidden")
        return opened, "Repository dialog did not open"
    if exercise == "settings-dialog":
        page.locator("main button:has-text('Add Relay Provider')").click()
        dialog = page.locator("[data-testid='relay-provider-dialog']")
        dialog.wait_for(state="visible")
        opened = dialog.is_visible()
        page.keyboard.press("Escape")
        dialog.wait_for(state="hidden")
        return opened, "Relay provider dialog did not open"
    if exercise == "offboarding-dialog":
        page.locator("[data-testid='disable-relay-user-7']").click()
        dialog = page.locator("[data-testid='offboarding-disable-dialog']")
        dialog.wait_for(state="visible")
        opened = dialog.is_visible()
        page.keyboard.press("Escape")
        dialog.wait_for(state="hidden")
        return opened, "Offboarding dialog did not open"
    return True, ""


def visit_matrix_case(page, role, case, viewport):
    viewport_name, width, height = viewport
    page.set_viewport_size({"width": width, "height": height})
    page_errors = []
    on_page_error = lambda error: page_errors.append(str(error))
    page.on("pageerror", on_page_error)
    path = case["path"]
    label = f"{role} {path.split('?')[0]} @ {width}"
    try:
        page.goto(f"{BASE}{path}")
        page.wait_for_load_state("networkidle")
        selector = expected_selector(case, width)
        critical = page.locator(selector).first
        critical.wait_for(state="visible", timeout=5000)
        exercised, exercise_detail = exercise_route_control(page, case.get("exercise"))
        overflow = overflow_state(page)
        expected_path = case.get("expected_path", path.split("?")[0])
        actual_path = urlparse(page.url).path
        checks = {
            "path": actual_path == expected_path,
            "critical": critical.is_visible(),
            "shell": protected_shell_state(page, width),
            "overflow": overflow["documentWidth"] <= overflow["viewport"],
            "interaction": exercised,
            "page_errors": not page_errors,
        }
        report(
            label,
            all(checks.values()),
            json.dumps({
                "viewport": viewport_name,
                "checks": checks,
                "overflow": overflow,
                "exercise": exercise_detail,
                "errors": page_errors,
                "url": page.url,
            }),
        )
    except Exception as error:
        report(label, False, str(error))
        screenshot(page, f"matrix_{role}_{viewport_name}_{path.split('?')[0].strip('/').replace('/', '_') or 'root'}")
    finally:
        page.remove_listener("pageerror", on_page_error)


def test_route_role_viewport_matrix(page):
    print("\n🧪 Active Routes — Role and Viewport Matrix")

    do_dev_login(page, role="user")
    for viewport in VIEWPORTS:
        for case in USER_ROUTE_CASES:
            visit_matrix_case(page, "user", case, viewport)

    page.evaluate("localStorage.clear()")
    do_dev_login(page, role="admin")
    for viewport in VIEWPORTS:
        for case in ADMIN_ROUTE_CASES:
            visit_matrix_case(page, "admin", case, viewport)

    mock_auth_endpoints(page, role="user")
    for viewport_name, width, height in VIEWPORTS:
        page.set_viewport_size({"width": width, "height": height})
        for case in PUBLIC_ROUTE_CASES:
            if case["authenticated"]:
                page.evaluate("""() => {
                    localStorage.setItem('token', 'user-token')
                    localStorage.setItem('refresh_token', 'user-refresh')
                }""")
            else:
                page.evaluate("localStorage.clear()")

            page_errors = []
            on_page_error = lambda error: page_errors.append(str(error))
            page.on("pageerror", on_page_error)
            path = case["path"]
            label = f"public {path.split('?')[0]} @ {width}"
            try:
                page.goto(f"{BASE}{path}")
                page.wait_for_load_state("networkidle")
                critical = page.locator(case["selector"]).first
                critical.wait_for(state="visible", timeout=5000)
                overflow = overflow_state(page)
                expected_path = case.get("expected_path", path.split("?")[0])
                checks = {
                    "path": urlparse(page.url).path == expected_path,
                    "critical": critical.is_visible(),
                    "auth_shell": page.locator("[data-testid='auth-language-toggle']").is_visible(),
                    "overflow": overflow["documentWidth"] <= overflow["viewport"],
                    "page_errors": not page_errors,
                }
                report(
                    label,
                    all(checks.values()),
                    json.dumps({"viewport": viewport_name, "checks": checks, "overflow": overflow, "url": page.url}),
                )
            except Exception as error:
                report(label, False, str(error))
                screenshot(page, f"matrix_public_{viewport_name}_{path.split('?')[0].strip('/').replace('/', '_')}")
            finally:
                page.remove_listener("pageerror", on_page_error)

    page.set_viewport_size({"width": 1440, "height": 900})
    page.evaluate("localStorage.clear()")
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
            ("Active Route Role/Viewport Matrix", lambda: test_route_role_viewport_matrix(page)),
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
