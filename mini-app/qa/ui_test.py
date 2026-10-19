"""Render the shipped UI in real Chromium with local, read-only synthetic APIs.

Run: python mini-app/qa/ui_test.py [--browser-path /usr/bin/chromium]
No production request, sign-in, request creation, or external navigation is made.
"""
from argparse import ArgumentParser
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit
import json
import os
import shutil
import threading

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / "mini-app/qa/results"
OUT.mkdir(exist_ok=True)
VIEWPORTS = {"mobile": {"width": 390, "height": 844}, "desktop": {"width": 1280, "height": 900}}


class Handler(SimpleHTTPRequestHandler):
    def do_GET(self):
        if not self.path.startswith("/assets/"):
            self.path = "/index.html"
        return super().do_GET()

    def log_message(self, *_args):
        pass


def fixtures():
    rows = []
    for index, (name, status, last, reply) in enumerate([
        ("Fixture success", "replied", "successful", True),
        ("Fixture refusal", "replied", "rejected", True),
        ("Fixture partial", "replied", "partially_successful", True),
        ("Fixture unrated", "replied", "waiting_classification", True),
        ("Fixture pending", "pending", "waiting_classification", False),
        ("Fixture bounced", "bounced", "error_message", True),
        ("Fixture unknown", "replied", "future_status", True),
    ]):
        rows.append({
            "id": str(index), "recipientName": name, "subject": name,
            "date": "2099-01-01T10:00:00Z", "status": status, "lastStatus": last,
            "replyReceivedAt": "2099-01-02T10:00:00Z" if reply else "",
            "delivered": True, "channel": "dostup",
            "responseExcerpt": "Fixture excerpt <img src=x onerror=alert(1)>",
            "url": f"https://dostup.org.ua/request/fixture_{index}",
        })
    return rows


def check_no_overflow(page):
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), f"horizontal overflow: {page.url}"


def exercise(page, origin, viewport, rows, checks):
    page.goto(origin + "/requests")
    page.get_by_text("Fixture refusal", exact=True).first.wait_for()
    assert page.locator(".sz-request-card").count() == len(rows)
    assert page.get_by_text("Відповідь без оцінки", exact=True).count() == 2
    for label, expected in [
        ("Відмови", ["Fixture refusal"]),
        ("Частково", ["Fixture partial"]),
        ("Відповіді", ["Fixture success", "Fixture refusal", "Fixture partial", "Fixture unrated", "Fixture unknown"]),
        ("Очікують", ["Fixture pending"]),
    ]:
        page.get_by_role("button", name=label, exact=True).click()
        body = page.locator(".sz-main").inner_text()
        for row in rows:
            assert (row["subject"] in body) == (row["subject"] in expected), (label, row["subject"], body)
    page.get_by_role("button", name="Усі", exact=True).click()
    check_no_overflow(page)
    page.screenshot(path=str(OUT / f"requests-{viewport}.png"), full_page=True)
    checks.append(f"{viewport}: status filters and request cards")

    page.locator(".sz-request-card").filter(has_text="Fixture refusal").click()
    page.wait_for_url("**/requests/1")
    page.locator(".sz-detail").wait_for()
    excerpt = page.locator(".sz-response-excerpt")
    assert excerpt.inner_text() == rows[1]["responseExcerpt"]
    assert excerpt.locator("img").count() == 0
    assert "Відповідь отримано" in page.locator(".sz-timeline").inner_text()
    assert page.locator(".sz-source-link").get_attribute("href") == rows[1]["url"]
    check_no_overflow(page)
    page.screenshot(path=str(OUT / f"detail-{viewport}.png"), full_page=True)
    checks.append(f"{viewport}: card-to-detail navigation, literal excerpt, recorded timeline")

    for request_id in ("4", "5"):
        page.goto(origin + "/requests/" + request_id)
        page.locator(".sz-timeline").wait_for()
        assert page.locator(".sz-response-excerpt").count() == 0
        assert "Відповідь отримано" not in page.locator(".sz-timeline").inner_text()
        check_no_overflow(page)
    page.goto(origin + "/requests/missing-fixture")
    page.get_by_role("heading", name="Запит недоступний", exact=True).wait_for()
    checks.append(f"{viewport}: pending, bounce and unknown-request details")

    page.goto(origin + "/rating")
    page.get_by_text("Fixture authority", exact=True).wait_for()
    body = page.locator("body").inner_text()
    assert "Успішних: 40" in body and "Відповідей:" not in body and "За даними порталу" in body
    check_no_overflow(page)
    page.screenshot(path=str(OUT / f"rating-{viewport}.png"), full_page=True)

    page.goto(origin + "/analytics")
    page.get_by_text("Зведені дані журналу", exact=False).wait_for()
    check_no_overflow(page)
    page.screenshot(path=str(OUT / f"analytics-{viewport}.png"), full_page=True)
    checks.append(f"{viewport}: accurate rating and journal analytics labels")

    page.goto(origin + "/")
    page.locator(".sz-latest-event").wait_for()
    assert "Fixture bounced" not in page.locator(".sz-latest-event").inner_text()
    assert page.locator(".sz-summary").get_by_text("Відповідей", exact=True).count() == 1
    assert page.locator(".sz-home .sz-next-action").get_attribute("href").startswith("/requests/")
    assert page.locator(".sz-home .sz-primary-action").get_attribute("href") == "https://t.me/Infozaputbot"
    check_no_overflow(page)
    page.screenshot(path=str(OUT / f"overview-{viewport}.png"), full_page=True)
    checks.append(f"{viewport}: overview summary, latest answer, next-step link, bot link")

    nav = page.locator(".sz-bottom-nav" if viewport == "mobile" else ".sz-sidebar nav")
    for label, path in [("Запити", "/requests"), ("Рейтинг", "/rating"), ("Аналітика", "/analytics"), ("Огляд", "/")]:
        nav.get_by_role("link", name=label, exact=True).click()
        page.wait_for_url(origin + path)
        assert nav.get_by_role("link", name=label, exact=True).get_attribute("aria-current") == "page"
    checks.append(f"{viewport}: all four navigation destinations and active states")


def main():
    parser = ArgumentParser()
    parser.add_argument("--browser-path", default=os.environ.get("CHROMIUM_EXECUTABLE") or shutil.which("chromium"))
    args = parser.parse_args()
    server = ThreadingHTTPServer(("127.0.0.1", 0), partial(Handler, directory=str(ROOT / "internal/web/static")))
    threading.Thread(target=server.serve_forever, daemon=True).start()
    origin = f"http://127.0.0.1:{server.server_port}"
    rows = fixtures()
    api = {
        "/api/requests": rows, "/api/me": {"firstName": "Тест"},
        "/api/portal-stats": {"requests": 100, "successful": 40, "organs": 20},
        "/api/rating": {"items": [{"name": "Fixture authority", "requests": 100, "successful": 40, "overdue": 7, "slug": "fixture"}]},
        "/api/analytics": {"total": 7, "answered": 5, "ackOnly": 1, "awaiting": 1, "bounced": 1},
    }
    report = {"passed": False, "browser": "Chromium", "viewports": VIEWPORTS, "fixtures": len(rows), "checks": [], "pageErrors": [], "unexpectedRequests": []}
    try:
        with sync_playwright() as playwright:
            launch = {"headless": True, "args": ["--no-sandbox"]}
            if args.browser_path:
                launch["executable_path"] = args.browser_path
            browser = playwright.chromium.launch(**launch)
            report["browserVersion"] = browser.version
            try:
                for viewport, dimensions in VIEWPORTS.items():
                    context = browser.new_context(viewport=dimensions, device_scale_factor=1)
                    def route(request):
                        url = request.request.url
                        path = urlsplit(url).path
                        if url == "https://telegram.org/js/telegram-web-app.js":
                            request.fulfill(body="", content_type="application/javascript")
                        elif url.startswith(origin + "/") and path.startswith("/api/"):
                            if request.request.method != "GET" or path not in api:
                                report["unexpectedRequests"].append(f"{request.request.method} {path}")
                                request.fulfill(status=404, json={"ok": False})
                            else:
                                request.fulfill(json={"ok": True, "data": api[path]})
                        elif url.startswith(origin + "/"):
                            request.continue_()
                        else:
                            report["unexpectedRequests"].append(url)
                            request.abort()
                    context.route("**/*", route)
                    page = context.new_page()
                    page.set_default_timeout(10000)
                    page.on("pageerror", lambda error: report["pageErrors"].append(str(error)))
                    exercise(page, origin, viewport, rows, report["checks"])
                    context.close()
                assert not report["pageErrors"], report["pageErrors"]
                assert not report["unexpectedRequests"], report["unexpectedRequests"]
                report["passed"] = True
            finally:
                browser.close()
    except Exception as error:
        report["error"] = str(error)
        raise
    finally:
        server.shutdown()
        server.server_close()
        (OUT / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print("UI fixtures passed; screenshots saved:", OUT)


if __name__ == "__main__":
    main()
