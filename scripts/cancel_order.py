#!/usr/bin/env python3
"""
Cancel an open order by order ID using the official Polymarket Python SDK.
Called by the Go bot via subprocess when an order has been live for too long
without filling.

Input (stdin or argv[1]):  {"order_id": "<polymarket_order_id>"}
Output (stdout):           {"success": true/false, "order_id": "...", "error": "..."}
"""
import sys
import json
import os
import time
from py_clob_client.client import ClobClient
from dotenv import load_dotenv

_CREDS_CACHE_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), ".api_creds_cache.json")
_CREDS_CACHE_TTL = 3600


def _load_cached_creds():
    try:
        if not os.path.exists(_CREDS_CACHE_FILE):
            return None
        with open(_CREDS_CACHE_FILE, "r") as f:
            cache = json.load(f)
        if time.time() - cache.get("timestamp", 0) > _CREDS_CACHE_TTL:
            return None
        return cache.get("creds")
    except Exception:
        return None


def _save_cached_creds(creds_dict):
    try:
        cache = {"timestamp": time.time(), "creds": creds_dict}
        with open(_CREDS_CACHE_FILE, "w") as f:
            json.dump(cache, f)
    except Exception:
        pass


def cancel_order(order_id: str) -> dict:
    try:
        load_dotenv()

        host = os.getenv("POLYMARKET_BASE_URL", "https://clob.polymarket.com")
        key = os.getenv("POLYMARKET_PRIVATE_KEY")
        chain_id = int(os.getenv("CHAIN_ID", "137"))
        funder = "0x6ff7ae88dbba1834f7647f4153fe30897904931d"

        if not key:
            return {"success": False, "error": "POLYMARKET_PRIVATE_KEY not set"}

        client = ClobClient(
            host,
            key=key,
            chain_id=chain_id,
            signature_type=1,
            funder=funder,
        )

        cached = _load_cached_creds()
        if cached:
            from py_clob_client.clob_types import ApiCreds
            creds = ApiCreds(
                api_key=cached["api_key"],
                api_secret=cached["api_secret"],
                api_passphrase=cached["api_passphrase"],
            )
            client.set_api_creds(creds)
        else:
            derived = client.create_or_derive_api_creds()
            client.set_api_creds(derived)
            _save_cached_creds({
                "api_key": derived.api_key,
                "api_secret": derived.api_secret,
                "api_passphrase": derived.api_passphrase,
            })

        resp = client.cancel(order_id)
        return {
            "success": True,
            "order_id": order_id,
            "response": resp,
        }

    except Exception as e:
        return {
            "success": False,
            "order_id": order_id,
            "error": str(e),
            "error_type": type(e).__name__,
        }


def main():
    try:
        if len(sys.argv) > 1:
            input_data = json.loads(sys.argv[1])
        else:
            input_data = json.load(sys.stdin)

        if "order_id" not in input_data:
            print(json.dumps({"success": False, "error": "Missing required field: order_id"}))
            sys.exit(1)

        result = cancel_order(input_data["order_id"])
        print(json.dumps(result))
        sys.exit(0 if result["success"] else 1)

    except json.JSONDecodeError as e:
        print(json.dumps({"success": False, "error": f"Invalid JSON input: {str(e)}"}))
        sys.exit(1)
    except Exception as e:
        print(json.dumps({"success": False, "error": str(e), "error_type": type(e).__name__}))
        sys.exit(1)


if __name__ == "__main__":
    main()
