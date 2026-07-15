"""Deterministic Playwright coverage for the multi-stage quota reset workflow."""

import copy
import json
import os
import signal
import shutil
import subprocess
import sys
from urllib.parse import urlparse

from playwright.sync_api import Route, sync_playwright


BASE = os.environ.get("BASE", "http://127.0.0.1:5173").rstrip("/")
SCREENSHOT_DIR = "/tmp/ae-e2e-quota-reset"
COMMENT = "Approved for the release investigation."
WECOM_ROBOT_URL = (
    "https://qyapi.weixin.qq.com/cgi-bin/webhook/send"
    "?key=synthetic-browser-robot-key"
)
WECOM_URL_PREVIEW = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send"
MAX_RUNTIME_SECONDS = 60
TIMEOUT_EXIT_STATUS = 124
WORKER_FLAG = "--worker"

USERS = {
    "requester": {
        "id": 101,
        "username": "alice",
        "email": "alice@example.com",
        "role": "user",
        "auth_source": "sso",
    },
    "approver": {
        "id": 202,
        "username": "bob",
        "email": "bob@example.com",
        "role": "user",
        "auth_source": "sso",
    },
    "future": {
        "id": 303,
        "username": "carol",
        "email": "carol@example.com",
        "role": "user",
        "auth_source": "sso",
    },
    "admin": {
        "id": 1,
        "username": "admin",
        "email": "admin@example.com",
        "role": "admin",
        "auth_source": "dev",
    },
}

DIRECTORY_SOURCE = {
    "id": 11,
    "name": "Synthetic Directory",
    "description": "Synthetic full-company directory for browser coverage",
    "scope": "full_company",
    "enabled": True,
    "dsl": "version: 1\nscope: full_company\nsteps: []\n",
    "schedule_enabled": False,
    "schedule_interval": "daily",
    "schedule_timezone": "UTC",
    "last_successful_run_id": 111,
    "last_run_id": 111,
}


def approver(user_id, name, email):
    return {
        "user_id": user_id,
        "display_name": name,
        "email": email,
        "source": "configured",
    }


ACTIVE_REQUEST = {
    "id": 321,
    "requester_user_id": 101,
    "requester_display_name": "Alice",
    "requester_email": "alice@example.com",
    "requester_department_paths": ["Department Alpha / Platform"],
    "provider_id": 7,
    "group_id": "group-alpha",
    "group_name": "Group Alpha",
    "group_platform": "openai",
    "reason": "Investigate a time-sensitive release build regression.",
    "status": "pending",
    "resolved_approver_user_ids": [202],
    "matched_department_paths": [],
    "created_at": "2026-07-14T01:00:00Z",
    "updated_at": "2026-07-14T01:00:00Z",
    "workflow": {
        "version": 2,
        "current_node": {
            "id": 456,
            "position": 0,
            "node_type": "requester_departments",
            "label": "Department Alpha",
            "departments": [{
                "external_id": "department-alpha",
                "display_path": "Department Alpha",
                "resolution": "configured",
            }],
            "status": "active",
            "admin_fallback_required": False,
            "approvers": [approver(202, "Bob", "bob@example.com")],
        },
        "nodes": [
            {
                "id": 456,
                "position": 0,
                "node_type": "requester_departments",
                "label": "Department Alpha",
                "departments": [{
                    "external_id": "department-alpha",
                    "display_path": "Department Alpha",
                    "resolution": "configured",
                }],
                "status": "active",
                "admin_fallback_required": False,
                "approvers": [approver(202, "Bob", "bob@example.com")],
            },
            {
                "id": 457,
                "position": 1,
                "node_type": "configured_department",
                "label": "Department Beta",
                "departments": [{
                    "external_id": "department-beta",
                    "display_path": "Department Beta",
                    "resolution": "configured",
                }],
                "status": "queued",
                "admin_fallback_required": False,
                "approvers": [approver(202, "Bob", "bob@example.com")],
            },
            {
                "id": 458,
                "position": 2,
                "node_type": "configured_department",
                "label": "Department Gamma",
                "departments": [{
                    "external_id": "department-gamma",
                    "display_path": "Department Gamma",
                    "resolution": "configured",
                }],
                "status": "queued",
                "admin_fallback_required": False,
                "approvers": [approver(303, "Carol", "carol@example.com")],
            },
            {
                "id": 459,
                "position": 3,
                "node_type": "configured_department",
                "label": "Department Delta",
                "departments": [{
                    "external_id": "department-delta",
                    "display_path": "Department Delta",
                    "resolution": "configured",
                }],
                "status": "queued",
                "admin_fallback_required": False,
                "approvers": [approver(202, "Bob", "bob@example.com")],
            },
        ],
        "decisions": [],
        "can_approve": True,
        "can_reject": False,
        "can_cancel": False,
        "can_retry": False,
    },
}

APPROVED_REQUEST = copy.deepcopy(ACTIVE_REQUEST)
APPROVED_REQUEST["updated_at"] = "2026-07-14T01:05:00Z"
APPROVED_REQUEST["workflow"]["current_node"] = copy.deepcopy(
    APPROVED_REQUEST["workflow"]["nodes"][2]
)
APPROVED_REQUEST["workflow"]["current_node"]["status"] = "active"
APPROVED_REQUEST["workflow"]["nodes"][0]["status"] = "approved"
APPROVED_REQUEST["workflow"]["nodes"][1]["status"] = "satisfied_by_prior_approval"
APPROVED_REQUEST["workflow"]["nodes"][1]["satisfied_by_decision_id"] = 900
APPROVED_REQUEST["workflow"]["nodes"][2]["status"] = "active"
APPROVED_REQUEST["workflow"]["nodes"][3]["status"] = "satisfied_by_prior_approval"
APPROVED_REQUEST["workflow"]["nodes"][3]["satisfied_by_decision_id"] = 900
APPROVED_REQUEST["workflow"]["decisions"] = [{
    "id": 900,
    "node_id": 456,
    "actor_user_id": 202,
    "actor_display_name": "Bob",
    "decision": "approve",
    "comment": COMMENT,
    "admin_override": False,
    "created_at": "2026-07-14T01:05:00Z",
}]
APPROVED_REQUEST["workflow"]["can_approve"] = False

CHAIN_ITEMS = [{
    "id": 71,
    "provider_id": 7,
    "group_id": "group-alpha",
    "group_name": "Group Alpha",
    "enabled": True,
    "nodes": [
        {
            "directory_source_id": 11,
            "department_external_id": "department-alpha",
            "department_display_path": "Department Alpha",
        },
        {
            "directory_source_id": 11,
            "department_external_id": "department-beta",
            "department_display_path": "Department Beta",
        },
    ],
}]


def api_response(data, code=200):
    return json.dumps({"code": code, "data": data})


def fulfill(route, data, status=200):
    route.fulfill(
        status=status,
        content_type="application/json",
        body=api_response(data),
    )


class SyntheticAPI:
    def __init__(self, session):
        self.session = session
        self.approver_request = copy.deepcopy(ACTIVE_REQUEST)
        self.chain_payloads = []
        self.notification_payloads = []
        self.decision_payloads = []

    def list_response(self, items):
        return {"items": items, "page": 1, "page_size": 20, "total": len(items)}

    def handle(self, route: Route):
        request = route.request
        parsed = urlparse(request.url)
        path = parsed.path
        method = request.method

        if path == "/api/v1/auth/me" and method == "GET":
            fulfill(route, USERS[self.session])
            return
        if path == "/api/v1/auth/options" and method == "GET":
            fulfill(route, {"ldap_enabled": True, "dev_login_enabled": True})
            return
        if path == "/api/v1/auth/refresh" and method == "POST":
            fulfill(route, {"token": f"{self.session}-token", "refresh_token": "test-refresh"})
            return
        if path == "/api/v1/auth/dev-login" and method == "POST":
            fulfill(route, {"token": f"{self.session}-token", "refresh_token": "test-refresh"})
            return
        if path == "/api/v1/work-items/counts" and method == "GET":
            approval_count = 1 if self.session == "approver" else 0
            admin_count = 1 if self.session == "admin" else 0
            fulfill(route, {
                "quota_reset_approval_count": approval_count,
                "quota_reset_admin_count": admin_count,
                "ai_access_setup_count": 0,
                "offboarding_count": 0,
                "total_count": approval_count + admin_count,
            })
            return
        if path == "/api/v1/user/quota-reset/requests" and method == "GET":
            items = [self.requester_view()] if self.session == "requester" else []
            fulfill(route, self.list_response(items))
            return
        if path == "/api/v1/user/quota-reset/approvals" and method == "GET":
            items = [self.approver_request] if self.session == "approver" else []
            fulfill(route, self.list_response(items))
            return
        if path == "/api/v1/admin/quota-reset/requests" and method == "GET":
            items = [self.admin_view()] if self.session == "admin" else []
            fulfill(route, self.list_response(items))
            return
        if path == "/api/v1/user/quota-reset/approvals/321/approve" and method == "POST":
            body = json.loads(request.post_data or "{}")
            assert body == {
                "request_node_id": 456,
                "decision_reason": COMMENT,
            }
            self.decision_payloads.append(body)
            self.approver_request = copy.deepcopy(APPROVED_REQUEST)
            fulfill(route, APPROVED_REQUEST)
            return

        if path == "/api/v1/scm-providers" and method == "GET":
            fulfill(route, {"items": [], "total": 0, "page": 1, "page_size": 20})
            return
        if path == "/api/v1/admin/providers" and method == "GET":
            fulfill(route, [])
            return
        if path == "/api/v1/admin/credentials" and method == "GET":
            fulfill(route, [])
            return
        if path == "/api/v1/system/version" and method in {"GET", "POST"}:
            fulfill(route, {
                "version": {"version": "v0.0.0-test", "commit": "test", "build_time": "2026-07-14T00:00:00Z"},
                "check_enabled": True,
                "checked": method == "POST",
                "update_available": False,
            })
            return
        if path == "/api/v1/admin/settings/ldap" and method == "GET":
            fulfill(route, {"url": "", "base_dn": "", "bind_dn": "", "user_filter": "", "tls": False})
            return
        if path == "/api/v1/admin/directory/sources" and method == "GET":
            fulfill(route, {"items": [DIRECTORY_SOURCE]})
            return
        if path == "/api/v1/admin/quota-reset/approver-configs" and method == "GET":
            fulfill(route, {"directory_source_id": 11, "items": []})
            return
        if path == "/api/v1/admin/quota-reset/approval-chain-options" and method == "GET":
            fulfill(route, {
                "groups": [
                    {"provider_id": 7, "group_id": "group-alpha", "group_name": "Group Alpha", "platform": "openai"},
                    {"provider_id": 7, "group_id": "group-beta", "group_name": "Group Beta", "platform": "anthropic"},
                ],
                "departments": [
                    {"directory_source_id": 11, "department_external_id": "department-alpha", "department_display_path": "Department Alpha", "approver_count": 1},
                    {"directory_source_id": 11, "department_external_id": "department-beta", "department_display_path": "Department Beta", "approver_count": 1},
                ],
            })
            return
        if path == "/api/v1/admin/quota-reset/approval-chains" and method == "GET":
            fulfill(route, {"items": CHAIN_ITEMS})
            return
        if path == "/api/v1/admin/quota-reset/approval-chains" and method == "PUT":
            body = json.loads(request.post_data or "{}")
            assert [node["department_external_id"] for node in body["items"][0]["nodes"]] == [
                "department-beta",
                "department-alpha",
            ]
            self.chain_payloads.append(body)
            saved = copy.deepcopy(body["items"])
            saved[0]["id"] = 71
            fulfill(route, {"items": saved})
            return
        if path == "/api/v1/admin/quota-reset/notification-settings" and method == "GET":
            fulfill(route, {
                "enabled": True,
                "channel_type": "generic_webhook",
                "template_version": 1,
                "url_configured": True,
                "url_preview": "https://hooks.example.com/.../quota-reset",
                "auth_type": "none",
                "credential_id": None,
            })
            return
        if path == "/api/v1/admin/quota-reset/notification-settings" and method == "PUT":
            body = json.loads(request.post_data or "{}")
            assert body == {
                "enabled": True,
                "channel_type": "wecom_group_robot",
                "auth_type": "none",
                "credential_id": None,
                "url": WECOM_ROBOT_URL,
            }
            self.notification_payloads.append(body)
            fulfill(route, {
                "enabled": True,
                "channel_type": "wecom_group_robot",
                "template_version": 1,
                "url_configured": True,
                "url_preview": WECOM_URL_PREVIEW,
                "auth_type": "none",
                "credential_id": None,
            })
            return

        route.fulfill(
            status=404,
            content_type="application/json",
            body=json.dumps({"code": 404, "message": f"unmocked synthetic route: {method} {path}"}),
        )

    def requester_view(self):
        item = copy.deepcopy(ACTIVE_REQUEST)
        item["workflow"]["can_approve"] = False
        item["workflow"]["can_cancel"] = True
        return item

    def admin_view(self):
        item = copy.deepcopy(ACTIVE_REQUEST)
        item["workflow"]["can_approve"] = True
        item["workflow"]["can_reject"] = True
        return item


def new_session(browser, session, viewport):
    context = browser.new_context(viewport=viewport)
    context.add_init_script(
        f"""
        localStorage.setItem('token', {json.dumps(f'{session}-token')});
        localStorage.setItem('refresh_token', 'test-refresh');
        localStorage.setItem('locale', 'en');
        """,
    )
    api = SyntheticAPI(session)
    page = context.new_page()
    page.set_default_timeout(8000)
    page.set_default_navigation_timeout(8000)
    page.route("**/api/v1/**", api.handle)
    return context, page, api


def wait_for_page(page):
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(150)


def assert_no_horizontal_overflow(page):
    overflow = page.evaluate(
        "() => document.documentElement.scrollWidth > document.documentElement.clientWidth"
    )
    assert not overflow, "page has horizontal overflow"


def assert_no_control_overlap(page, root_selector="body"):
    overlaps = page.evaluate(
        """(rootSelector) => {
            const root = document.querySelector(rootSelector);
            if (!root) return [`missing root ${rootSelector}`];
            const selector = [
              'button',
              'input',
              'select',
              'textarea',
              'a[href]',
              '[role="button"]',
              '[role="link"]',
              '[tabindex]',
            ].join(',');
            const controls = [...new Set(root.querySelectorAll(selector))]
              .filter((element) => {
                const rect = element.getBoundingClientRect();
                const style = getComputedStyle(element);
                const selectedByTabIndex = element.hasAttribute('tabindex') && element.tabIndex >= 0;
                const selectedBySemantics = element.matches(
                  'button, input, select, textarea, a[href], [role="button"], [role="link"]'
                );
                return (selectedBySemantics || selectedByTabIndex)
                  && rect.width > 0
                  && rect.height > 0
                  && style.visibility !== 'hidden'
                  && style.display !== 'none';
              });
            const failures = [];
            for (let leftIndex = 0; leftIndex < controls.length; leftIndex += 1) {
              const left = controls[leftIndex];
              const leftRect = left.getBoundingClientRect();
              for (let rightIndex = leftIndex + 1; rightIndex < controls.length; rightIndex += 1) {
                const right = controls[rightIndex];
                if (left.contains(right) || right.contains(left)) continue;
                const rightRect = right.getBoundingClientRect();
                const width = Math.min(leftRect.right, rightRect.right) - Math.max(leftRect.left, rightRect.left);
                const height = Math.min(leftRect.bottom, rightRect.bottom) - Math.max(leftRect.top, rightRect.top);
                if (width > 1 && height > 1) {
                  failures.push(`${left.tagName}:${left.getAttribute('data-testid') || left.textContent?.trim().slice(0, 30)} overlaps ${right.tagName}:${right.getAttribute('data-testid') || right.textContent?.trim().slice(0, 30)}`);
                }
              }
            }
            return failures;
        }""",
        root_selector,
    )
    assert not overlaps, "control overlap: " + "; ".join(overlaps[:5])


def screenshot(page, name):
    page.screenshot(path=os.path.join(SCREENSHOT_DIR, name), full_page=False)


def test_active_approver(browser):
    context, page, api = new_session(browser, "approver", {"width": 1280, "height": 800})
    try:
        page.goto(f"{BASE}/usage/quota-reset")
        wait_for_page(page)
        page.get_by_test_id("quota-reset-tab-approvals").click()

        badge = page.get_by_test_id("quota-reset-tab-approvals-count")
        assert badge.inner_text().strip() == "1"
        row = page.get_by_test_id("quota-reset-row-321")
        assert row.count() == 1
        approve = page.get_by_test_id("quota-reset-approve-321")
        assert approve.is_enabled()
        enabled_actions = row.locator(
            "[data-testid^='quota-reset-approve-']:enabled, "
            "[data-testid^='quota-reset-reject-']:enabled, "
            "[data-testid^='quota-reset-cancel-']:enabled, "
            "[data-testid^='quota-reset-retry-']:enabled"
        )
        action_ids = enabled_actions.evaluate_all(
            "elements => elements.map(element => element.getAttribute('data-testid'))"
        )
        assert action_ids == ["quota-reset-approve-321"], (
            f"enabled workflow actions = {action_ids}, want only quota-reset-approve-321"
        )
        assert_no_horizontal_overflow(page)
        assert_no_control_overlap(page)
        screenshot(page, "01-active-approver-desktop-1280x800.png")

        approve.click()
        dialog = page.get_by_test_id("quota-reset-decision-dialog")
        dialog.wait_for(state="visible")
        page.get_by_test_id("quota-reset-decision-submit").click()
        alert = dialog.get_by_role("alert")
        assert alert.is_visible()
        assert api.decision_payloads == []
        page.locator("#quota-reset-decision-comment").fill(COMMENT)
        assert_no_control_overlap(page, "[data-testid='quota-reset-decision-dialog']")
        screenshot(page, "02-required-comment-desktop-1280x800.png")
        page.get_by_test_id("quota-reset-decision-submit").click()
        page.get_by_test_id("quota-reset-decision-dialog").wait_for(state="detached")
        assert api.decision_payloads == [{"request_node_id": 456, "decision_reason": COMMENT}]

        page.get_by_test_id("quota-reset-view-details-321").click()
        detail = page.get_by_test_id("quota-reset-detail-dialog")
        detail.wait_for(state="visible")
        assert detail.get_by_text("Satisfied by prior approval", exact=True).count() == 2
        assert detail.get_by_text(f"Reused approval from Bob: {COMMENT}", exact=True).count() == 2
        assert_no_horizontal_overflow(page)
        assert_no_control_overlap(page, "[data-testid='quota-reset-detail-dialog']")
        screenshot(page, "03-reused-approval-desktop-1280x800.png")
    finally:
        context.close()


def test_future_approver(browser):
    context, page, _api = new_session(browser, "future", {"width": 1280, "height": 800})
    try:
        page.goto(f"{BASE}/usage/quota-reset")
        wait_for_page(page)
        page.get_by_test_id("quota-reset-tab-approvals").click()
        assert page.get_by_test_id("quota-reset-tab-approvals-count").count() == 0
        assert page.locator("[data-testid^='quota-reset-approve-']").count() == 0
        assert page.locator("[data-testid^='quota-reset-row-']").count() == 0
        assert_no_horizontal_overflow(page)
        assert_no_control_overlap(page)
    finally:
        context.close()


def test_requester_keyboard_and_mobile(browser):
    context, page, _api = new_session(browser, "requester", {"width": 390, "height": 844})
    try:
        page.goto(f"{BASE}/usage/quota-reset")
        wait_for_page(page)
        opener = page.get_by_role("button", name="View details for Group Alpha", exact=True)
        assert opener.count() == 1
        assert opener.evaluate("element => element.tagName === 'BUTTON'")
        assert opener.get_attribute("data-testid") == "quota-reset-view-details-321"
        opener.focus()
        opener.press("Enter")
        detail = page.get_by_test_id("quota-reset-detail-dialog")
        detail.wait_for(state="visible")
        close_button = page.get_by_test_id("quota-reset-detail-close")
        assert close_button.evaluate("element => document.activeElement === element")
        close_button.press("Tab")
        assert detail.evaluate("element => element.contains(document.activeElement)")
        assert close_button.evaluate("element => document.activeElement === element")
        close_button.press("Shift+Tab")
        assert detail.evaluate("element => element.contains(document.activeElement)")
        assert close_button.evaluate("element => document.activeElement === element")
        assert_no_horizontal_overflow(page)
        assert_no_control_overlap(page, "[data-testid='quota-reset-detail-dialog']")
        screenshot(page, "04-requester-detail-mobile-390x844.png")
        close_button.press("Escape")
        detail.wait_for(state="detached")
        assert opener.evaluate("element => document.activeElement === element")
        assert_no_horizontal_overflow(page)
        assert_no_control_overlap(page)
    finally:
        context.close()


def test_admin_settings(browser, viewport, screenshot_name):
    context, page, api = new_session(browser, "admin", viewport)
    try:
        page.goto(f"{BASE}/settings?section=organization-login")
        wait_for_page(page)
        settings = page.get_by_test_id("quota-reset-approval-settings")
        settings.wait_for(state="visible")
        settings_text = settings.inner_text()
        assert "Failed to load department approvers" not in settings_text
        assert "Failed to load approval chains" not in settings_text
        assert "Failed to load notification settings" not in settings_text

        page.get_by_test_id("quota-reset-chain-group-select").click()
        page.get_by_test_id("quota-reset-chain-group-option-7-group-alpha").click()
        page.get_by_test_id("quota-reset-chain-move-down-department-alpha").click()
        nodes = page.locator("[data-testid^='quota-reset-chain-node-']")
        assert nodes.count() == 2
        assert nodes.nth(0).get_attribute("data-testid") == "quota-reset-chain-node-department-beta"
        page.get_by_test_id("quota-reset-save-chains").click()
        page.wait_for_function("() => document.body.innerText.includes('Approval chains saved')")
        assert len(api.chain_payloads) == 1

        channel = page.get_by_test_id("quota-reset-notification-channel")
        channel.select_option("wecom_group_robot")
        assert channel.input_value() == "wecom_group_robot"
        endpoint = page.get_by_test_id("quota-reset-notification-url")
        endpoint.fill(WECOM_ROBOT_URL)
        assert endpoint.input_value() == WECOM_ROBOT_URL
        page.get_by_test_id("quota-reset-save-notification").click()
        page.wait_for_function("() => document.body.innerText.includes('Notification settings saved')")
        assert len(api.notification_payloads) == 1
        preview = page.get_by_test_id("quota-reset-notification-preview")
        assert "Quota reset approval pending" in preview.inner_text()
        assert "@Bob" in preview.inner_text()
        settings_text = settings.inner_text()
        assert WECOM_REDACTED_PREVIEW in settings_text
        assert "synthetic-browser-robot-key" not in settings_text

        chains = page.get_by_test_id("subscription-group-approval-chains")
        chains.scroll_into_view_if_needed()
        if viewport["width"] < 640:
            page.evaluate(
                """() => {
                    const section = document.querySelector('[data-testid="subscription-group-approval-chains"]');
                    window.scrollTo(0, section.getBoundingClientRect().top + window.scrollY - 70);
                }"""
            )
        assert_no_horizontal_overflow(page)
        assert_no_control_overlap(page, "[data-testid='quota-reset-approval-settings']")
        screenshot(page, screenshot_name)
    finally:
        context.close()


def run_all():
    shutil.rmtree(SCREENSHOT_DIR, ignore_errors=True)
    os.makedirs(SCREENSHOT_DIR, exist_ok=True)
    tests = []

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        cases = [
            ("active approver workflow", lambda: test_active_approver(browser)),
            ("future approver invisibility", lambda: test_future_approver(browser)),
            ("requester keyboard detail and mobile layout", lambda: test_requester_keyboard_and_mobile(browser)),
            ("admin settings desktop", lambda: test_admin_settings(
                browser,
                {"width": 1280, "height": 800},
                "05-admin-settings-desktop-1280x800.png",
            )),
            ("admin settings mobile", lambda: test_admin_settings(
                browser,
                {"width": 390, "height": 844},
                "06-admin-settings-mobile-390x844.png",
            )),
        ]
        for name, case in cases:
            try:
                case()
                tests.append((name, True, ""))
                print(f"PASS: {name}")
            except Exception as error:
                import traceback

                tests.append((name, False, str(error)))
                print(f"FAIL: {name}: {error}")
                traceback.print_exc()
        browser.close()

    failures = [result for result in tests if not result[1]]
    print(f"Results: {len(tests) - len(failures)}/{len(tests)} passed")
    print(f"Screenshots: {SCREENSHOT_DIR}")
    return not failures


def _write_worker_output(stdout, stderr):
    if stdout:
        sys.stdout.write(stdout)
        sys.stdout.flush()
    if stderr:
        sys.stderr.write(stderr)
        sys.stderr.flush()


def _terminate_worker(process):
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass

    try:
        return process.communicate(timeout=5)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        return process.communicate()


def run_supervised(command, timeout_seconds=MAX_RUNTIME_SECONDS):
    process = subprocess.Popen(
        command,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        start_new_session=True,
        env={**os.environ, "PYTHONUNBUFFERED": "1"},
    )
    try:
        stdout, stderr = process.communicate(timeout=timeout_seconds)
    except subprocess.TimeoutExpired:
        stdout, stderr = _terminate_worker(process)
        _write_worker_output(stdout, stderr)
        print(
            f"FAIL: browser workflow worker exceeded {timeout_seconds} seconds",
            file=sys.stderr,
            flush=True,
        )
        return TIMEOUT_EXIT_STATUS
    except KeyboardInterrupt:
        stdout, stderr = _terminate_worker(process)
        _write_worker_output(stdout, stderr)
        return 130

    _write_worker_output(stdout, stderr)
    return process.returncode


def main(argv):
    if argv == [WORKER_FLAG]:
        return 0 if run_all() else 1
    if argv:
        print(f"Usage: {os.path.basename(__file__)}", file=sys.stderr)
        return 2

    worker_command = [sys.executable, "-u", os.path.abspath(__file__), WORKER_FLAG]
    return run_supervised(worker_command)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
