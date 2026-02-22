#!/usr/bin/env python3
"""
Test script to see exact order payload format used by official Python SDK
"""
import os
from py_clob_client.client import ClobClient
from py_clob_client.clob_types import OrderArgs
from dotenv import load_dotenv
import json

load_dotenv()

# Initialize client
host = os.getenv("POLYMARKET_BASE_URL", "https://clob.polymarket.com")
key = os.getenv("POLYMARKET_PRIVATE_KEY")
chain_id = int(os.getenv("CHAIN_ID", "137"))

client = ClobClient(
    host,
    key=key,
    chain_id=chain_id,
    signature_type=1,  # POLY_PROXY for Magic wallets
)

# Derive API credentials (same as Go bot)
client.set_api_creds(client.create_or_derive_api_creds())

print("✅ Client initialized with derived credentials")
print(f"API Key: {client.creds.api_key[:20]}...")

# Create a test order (don't actually place it)
# Using a weather market token ID from the logs
token_id = "40493458494401355045186060550211818867549971632050739682882482895097665450673"

order_args = OrderArgs(
    price=0.90,
    size=20.0,
    side="BUY",
    token_id=token_id,
)

# Build the signed order
try:
    signed_order = client.create_order(order_args)

    print("\n" + "="*60)
    print("SIGNED ORDER STRUCTURE:")
    print("="*60)
    print(json.dumps(signed_order, indent=2))
    print("\n")

    # This is what would be sent to the API
    print("="*60)
    print("PAYLOAD SENT TO /order ENDPOINT:")
    print("="*60)

    # The Python SDK likely wraps this, let's see what it sends
    # We can inspect the request by monkey-patching or looking at source

except Exception as e:
    print(f"Error creating order: {e}")
    import traceback
    traceback.print_exc()
