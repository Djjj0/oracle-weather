#!/usr/bin/env python3
"""
Python helper script to place orders using official Polymarket SDK
Called by Go bot via subprocess for reliable order placement
"""
import sys
import json
import os
from py_clob_client.client import ClobClient
from py_clob_client.clob_types import OrderArgs
from dotenv import load_dotenv

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

        # Derive API credentials
        client.set_api_creds(client.create_or_derive_api_creds())

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
