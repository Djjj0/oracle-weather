#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Diagnostic script to test Polymarket Python SDK configuration
"""
import os
import sys
from py_clob_client.client import ClobClient
from py_clob_client.clob_types import OrderArgs
from dotenv import load_dotenv

# Set UTF-8 encoding for Windows console
if sys.platform == 'win32':
    import codecs
    sys.stdout = codecs.getwriter('utf-8')(sys.stdout.buffer, 'strict')
    sys.stderr = codecs.getwriter('utf-8')(sys.stderr.buffer, 'strict')

load_dotenv()

print("Testing Polymarket Python SDK Configuration")
print("=" * 60)

# Get configuration
host = os.getenv("POLYMARKET_BASE_URL", "https://clob.polymarket.com")
private_key = os.getenv("POLYMARKET_PRIVATE_KEY")
chain_id = int(os.getenv("CHAIN_ID", "137"))

print(f"Host: {host}")
print(f"Chain ID: {chain_id}")
print(f"Private Key: {private_key[:10]}...{private_key[-10:]}")
print()

# Test 1: Create client with signature_type=1 (POLY_PROXY for Magic wallets)
print("Test 1: Creating client with signature_type=1 (POLY_PROXY)...")
try:
    funder_address = "0x6ff7ae88dbba1834f7647f4153fe30897904931d"  # Proxy wallet
    client = ClobClient(
        host,
        key=private_key,
        chain_id=chain_id,
        signature_type=1,
        funder=funder_address,  # Required for Magic wallets!
    )
    print("[OK] Client created successfully")
    print(f"   Funder (proxy): {funder_address}")
except Exception as e:
    print(f"[FAIL] Failed to create client: {e}")
    exit(1)

# Test 2: Derive API credentials
print("\nTest 2: Deriving API credentials...")
try:
    creds = client.create_or_derive_api_creds()
    client.set_api_creds(creds)
    print(f"[OK] API credentials derived")
    print(f"   API Key: {creds.api_key[:20]}...")
    print(f"   Passphrase: {creds.api_passphrase[:20]}...")
except Exception as e:
    print(f"[FAIL] Failed to derive credentials: {e}")
    exit(1)

# Test 3: Get API keys (test read access)
print("\nTest 3: Testing read access (get API keys)...")
try:
    keys = client.get_api_keys()
    print(f"[OK] Read access working - found {len(keys)} API keys")
    for key in keys:
        print(f"   - {key.get('apiKey', 'N/A')[:20]}...")
except Exception as e:
    print(f"[FAIL] Read access failed: {e}")

# Test 4: Check if wallet has allowance
print("\nTest 4: Checking USDC allowance...")
try:
    # Get user's wallet address
    from eth_account import Account
    pk = private_key
    if pk.startswith('0x'):
        pk = pk[2:]
    account = Account.from_key(pk)
    wallet_address = account.address
    print(f"   Wallet address (EOA): {wallet_address}")

    # Check allowance
    allowances = client.get_allowances()
    print(f"[OK] Allowance check successful")
    print(f"   Allowances: {allowances}")
except Exception as e:
    print(f"[WARN]  Could not check allowance: {e}")

# Test 5: Try creating an order (without posting)
print("\nTest 5: Creating a test order (not posting)...")
try:
    # Use a real token ID from the bot logs
    test_token_id = "103137592301487277392208161483193887445189877413281224192717026641880733766410"

    order_args = OrderArgs(
        price=0.50,
        size=5.0,
        side="BUY",
        token_id=test_token_id,
    )

    signed_order = client.create_order(order_args)
    print("[OK] Order created and signed successfully")

    # Test 6: Try posting the order (this is where it might fail)
    print("\nTest 6: Attempting to post order to exchange...")
    resp = client.post_order(signed_order, "GTC")
    print("[OK][OK][OK] ORDER POSTED SUCCESSFULLY! [OK][OK][OK]")
    print(f"   Order ID: {resp.get('orderID', 'N/A')}")
    print(f"   Status: {resp.get('status', 'N/A')}")
    print(f"   Response: {resp}")
except Exception as e:
    print(f"[FAIL] Failed to post order: {e}")
    print(f"   Error type: {type(e).__name__}")

    # Try to get more details
    if hasattr(e, 'status_code'):
        print(f"   Status code: {e.status_code}")
    if hasattr(e, 'error_message'):
        print(f"   Error message: {e.error_message}")

    # Try with signature_type=0 (EOA) instead
    print("\nTest 6b: Retrying with signature_type=0 (EOA)...")
    try:
        client2 = ClobClient(
            host,
            key=private_key,
            chain_id=chain_id,
            signature_type=0,  # Try EOA instead of POLY_PROXY
        )
        client2.set_api_creds(client2.create_or_derive_api_creds())

        order_args2 = OrderArgs(
            price=0.50,
            size=5.0,
            side="BUY",
            token_id=test_token_id,
        )

        signed_order2 = client2.create_order(order_args2)
        resp2 = client2.post_order(signed_order2, "GTC")

        print("[OK][OK][OK] ORDER POSTED WITH signature_type=0! [OK][OK][OK]")
        print(f"   Order ID: {resp2.get('orderID', 'N/A')}")
        print(f"   This means you should use signature_type=0, not 1")

    except Exception as e2:
        print(f"[FAIL] Also failed with signature_type=0: {e2}")

print("\n" + "=" * 60)
print("Diagnostic complete!")
