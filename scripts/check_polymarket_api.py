#!/usr/bin/env python3
"""
Small Polymarket API health check.

Checks:
- Gamma API: fetch one active market
- CLOB API: fetch an order book for a token ID

By default the script discovers a token ID from Gamma. You can also pass
--token-id to test a specific market directly.

Exit codes:
- 0: Gamma and CLOB look healthy
- 1: Partial outage or degraded state
- 2: Both checks failed or a full outage is likely
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from typing import Any


DEFAULT_CLOB_URL = os.getenv("POLYMARKET_BASE_URL", "https://clob.polymarket.com")
DEFAULT_GAMMA_URL = os.getenv("POLYMARKET_GAMMA_URL", "https://gamma-api.polymarket.com")
USER_AGENT = "oracle-weather-polymarket-healthcheck/1.0"


@dataclass
class CheckResult:
    name: str
    ok: bool
    url: str
    status_code: int | None
    latency_ms: int | None
    details: str


def fetch_json(url: str, timeout: float) -> tuple[int | None, int | None, Any, str]:
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    started = time.perf_counter()

    try:
        with urllib.request.urlopen(req, timeout=timeout) as response:
            body = response.read()
            latency_ms = int((time.perf_counter() - started) * 1000)
            parsed = json.loads(body.decode("utf-8"))
            return response.getcode(), latency_ms, parsed, ""
    except urllib.error.HTTPError as exc:
        latency_ms = int((time.perf_counter() - started) * 1000)
        body = exc.read().decode("utf-8", errors="replace")
        return exc.code, latency_ms, None, trim_text(body)
    except urllib.error.URLError as exc:
        latency_ms = int((time.perf_counter() - started) * 1000)
        return None, latency_ms, None, str(exc.reason)
    except TimeoutError:
        latency_ms = int((time.perf_counter() - started) * 1000)
        return None, latency_ms, None, "request timed out"
    except json.JSONDecodeError as exc:
        latency_ms = int((time.perf_counter() - started) * 1000)
        return None, latency_ms, None, f"invalid JSON: {exc}"


def trim_text(value: str, max_len: int = 160) -> str:
    text = " ".join(value.split())
    if len(text) <= max_len:
        return text
    return text[: max_len - 3] + "..."


def parse_string_list(value: Any) -> list[str]:
    if isinstance(value, list):
        return [str(item) for item in value]

    if isinstance(value, str):
        try:
            decoded = json.loads(value)
            if isinstance(decoded, list):
                return [str(item) for item in decoded]
        except json.JSONDecodeError:
            if value.strip():
                return [value.strip()]

    return []


def first_market(payload: Any) -> dict[str, Any] | None:
    if isinstance(payload, list) and payload:
        first = payload[0]
        return first if isinstance(first, dict) else None

    if isinstance(payload, dict):
        for key in ("data", "markets"):
            value = payload.get(key)
            if isinstance(value, list) and value:
                first = value[0]
                return first if isinstance(first, dict) else None

    return None


def extract_token_id(payload: Any) -> str | None:
    market = first_market(payload)
    if not market:
        return None

    for key in ("clobTokenIds", "clob_token_ids", "tokenIds", "token_ids"):
        token_ids = parse_string_list(market.get(key))
        if token_ids:
            return token_ids[0]

    tokens = market.get("tokens")
    if isinstance(tokens, list):
        for token in tokens:
            if not isinstance(token, dict):
                continue
            token_id = token.get("token_id") or token.get("tokenId") or token.get("id")
            if token_id:
                return str(token_id)

    return None


def check_gamma(base_url: str, timeout: float) -> tuple[CheckResult, str | None]:
    params = urllib.parse.urlencode({"closed": "false", "limit": "1"})
    url = f"{base_url.rstrip('/')}/markets?{params}"
    status, latency_ms, payload, error_text = fetch_json(url, timeout)

    if status != 200:
        details = error_text or f"unexpected status {status}"
        return CheckResult("Gamma", False, url, status, latency_ms, details), None

    market = first_market(payload)
    token_id = extract_token_id(payload)
    if not market:
        return CheckResult("Gamma", False, url, status, latency_ms, "response was valid JSON but no markets were returned"), None

    question = trim_text(str(market.get("question", "unknown market")), max_len=80)
    details = f"market ok: {question}"
    if not token_id:
        details += "; token id not found for CLOB follow-up"

    return CheckResult("Gamma", True, url, status, latency_ms, details), token_id


def check_clob(base_url: str, token_id: str, timeout: float) -> CheckResult:
    params = urllib.parse.urlencode({"token_id": token_id})
    url = f"{base_url.rstrip('/')}/book?{params}"
    status, latency_ms, payload, error_text = fetch_json(url, timeout)

    if status != 200:
        details = error_text or f"unexpected status {status}"
        return CheckResult("CLOB", False, url, status, latency_ms, details)

    if not isinstance(payload, dict):
        return CheckResult("CLOB", False, url, status, latency_ms, "response was not a JSON object")

    bids = payload.get("bids")
    asks = payload.get("asks")
    if not isinstance(bids, list) or not isinstance(asks, list):
        return CheckResult("CLOB", False, url, status, latency_ms, "missing bids/asks in order book response")

    details = f"orderbook ok: {len(bids)} bids, {len(asks)} asks"
    return CheckResult("CLOB", True, url, status, latency_ms, details)


def print_result(result: CheckResult) -> None:
    status = "UP" if result.ok else "DOWN"
    code = result.status_code if result.status_code is not None else "n/a"
    latency = f"{result.latency_ms}ms" if result.latency_ms is not None else "n/a"
    print(f"[{status}] {result.name}: status={code}, latency={latency}")
    print(f"      {result.details}")
    print(f"      {result.url}")


def main() -> int:
    parser = argparse.ArgumentParser(description="Check whether Polymarket's public APIs look healthy.")
    parser.add_argument("--gamma-url", default=DEFAULT_GAMMA_URL, help="Gamma API base URL")
    parser.add_argument("--clob-url", default=DEFAULT_CLOB_URL, help="CLOB API base URL")
    parser.add_argument("--token-id", help="Token ID to use for the CLOB orderbook check")
    parser.add_argument("--timeout", type=float, default=8.0, help="Per-request timeout in seconds")
    args = parser.parse_args()

    print("Polymarket API health check")
    print("=" * 30)

    gamma_result, discovered_token_id = check_gamma(args.gamma_url, args.timeout)
    print_result(gamma_result)

    token_id = args.token_id or discovered_token_id
    clob_result: CheckResult | None = None

    if token_id:
        clob_result = check_clob(args.clob_url, token_id, args.timeout)
        print_result(clob_result)
    else:
        print("[SKIP] CLOB: no token id available")

    if gamma_result.ok and clob_result and clob_result.ok:
        print("Overall: Polymarket public APIs look healthy.")
        return 0

    if gamma_result.ok or (clob_result and clob_result.ok):
        print("Overall: partial outage or degraded state detected.")
        return 1

    print("Overall: both checks failed; Polymarket may be down.")
    return 2


if __name__ == "__main__":
    sys.exit(main())