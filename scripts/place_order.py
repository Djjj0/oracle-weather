#!/usr/bin/env python3
"""
Python helper script to place orders using official Polymarket SDK
Called by Go bot via subprocess for reliable order placement
"""
import sys
import json
import os
import time
from py_clob_client.client import ClobClient
from py_clob_client.clob_types import OrderArgs
from dotenv import load_dotenv

# Cache file for derived API credentials (avoids slow re-derivation on every call)
_CREDS_CACHE_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), ".api_creds_cache.json")
_CREDS_CACHE_TTL = 3600  # 1 hour in seconds


def _load_cached_creds():
    """Load cached API credentials if they exist and are not expired."""
    try:
        if not os.path.exists(_CREDS_CACHE_FILE):
            return None
        with open(_CREDS_CACHE_FILE, "r") as f:
            cache = json.load(f)
        if time.time() - cache.get("timestamp", 0) > _CREDS_CACHE_TTL:
            return None  # Expired
        return cache.get("creds")
    except Exception:
        return None


def _save_cached_creds(creds_dict):
    """Save API credentials to cache file."""
    try:
        cache = {"timestamp": time.time(), "creds": creds_dict}
        with open(_CREDS_CACHE_FILE, "w") as f:
            json.dump(cache, f)
    except Exception:
        pass  # Caching is best-effort

def place_order(token_id: str, price: float, size: float, side: str) -> dict:
    """
    Place an order using the official Polymarket Python SDK

    Args:
        token_id: Token ID for the market outcome
        price: Limit price (0.01-0.99)
        size: Position size in USDC
        side: "BUY" or "SELL"

    Returns:
        dict with success status and order details or error message
    """
    try:
        # Load environment variables
        load_dotenv()

        # Initialize client with Magic wallet settings
        host = os.getenv("POLYMARKET_BASE_URL", "https://clob.polymarket.com")
        key = os.getenv("POLYMARKET_PRIVATE_KEY")
        chain_id = int(os.getenv("CHAIN_ID", "137"))
        funder = "0x6ff7ae88dbba1834f7647f4153fe30897904931d"  # Proxy wallet address

        if not key:
            return {
                "success": False,
                "error": "POLYMARKET_PRIVATE_KEY not set in environment"
            }

        # Create client with Magic wallet configuration
        client = ClobClient(
            host,
            key=key,
            chain_id=chain_id,
            signature_type=1,  # Magic/Email wallet
            funder=funder,     # Proxy wallet that holds the funds
        )

        # Derive API credentials (cached to avoid slow RPC calls on every order)
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

        # Create order
        order_args = OrderArgs(
            price=price,
            size=size,
            side=side,
            token_id=token_id,
        )

        # Place order
        signed_order = client.create_order(order_args)
        resp = client.post_order(signed_order, "GTC")  # Good-til-cancelled

        return {
            "success": True,
            "order_id": resp.get("orderID", ""),
            "status": resp.get("status", ""),
            "response": resp
        }

    except Exception as e:
        return {
            "success": False,
            "error": str(e),
            "error_type": type(e).__name__
        }

def main():
    """
    Main function - expects JSON input from Go via stdin
    Format: {"token_id": "...", "price": 0.5, "size": 20.0, "side": "BUY"}
    """
    try:
        # Read JSON input from stdin
        if len(sys.argv) > 1:
            # Accept as command line argument
            input_data = json.loads(sys.argv[1])
        else:
            # Read from stdin
            input_data = json.load(sys.stdin)

        # Validate required fields
        required_fields = ["token_id", "price", "size", "side"]
        for field in required_fields:
            if field not in input_data:
                print(json.dumps({
                    "success": False,
                    "error": f"Missing required field: {field}"
                }))
                sys.exit(1)

        # Place order
        result = place_order(
            token_id=input_data["token_id"],
            price=float(input_data["price"]),
            size=float(input_data["size"]),
            side=input_data["side"]
        )

        # Output result as JSON
        print(json.dumps(result))
        sys.exit(0 if result["success"] else 1)

    except json.JSONDecodeError as e:
        print(json.dumps({
            "success": False,
            "error": f"Invalid JSON input: {str(e)}"
        }))
        sys.exit(1)
    except Exception as e:
        print(json.dumps({
            "success": False,
            "error": str(e),
            "error_type": type(e).__name__
        }))
        sys.exit(1)

if __name__ == "__main__":
    main()
