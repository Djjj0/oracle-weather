#!/usr/bin/env python3
"""
Redeem a resolved Polymarket position on-chain.

Polymarket positions are ERC-1155 conditional tokens held in the proxy wallet.
When a market resolves, winning tokens can be redeemed for USDC by calling
redeemPositions() on the CTF (ConditionalTokens) contract.

Input JSON: {"token_id": "0x...", "outcome": "Yes"}
Output JSON: {"success": true/false, "tx_hash": "...", "error": "..."}
"""
import json
import os
import sys
from web3 import Web3
from eth_account import Account
from dotenv import load_dotenv

load_dotenv()

# ── Polygon contract addresses ────────────────────────────────────────────────
CTF_ADDRESS   = Web3.to_checksum_address("0x4D97DCd97eC945f40cF65F87097ACe5EA0476045")
USDC_ADDRESS  = Web3.to_checksum_address("0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174")
ZERO_BYTES32  = b"\x00" * 32

# Minimal ABI — only the functions we need
CTF_ABI = [
    {
        "name": "redeemPositions",
        "type": "function",
        "inputs": [
            {"name": "collateralToken", "type": "address"},
            {"name": "parentCollectionId", "type": "bytes32"},
            {"name": "conditionId", "type": "bytes32"},
            {"name": "indexSets", "type": "uint256[]"},
        ],
        "outputs": [],
        "stateMutability": "nonpayable",
    },
    {
        "name": "getConditionId",
        "type": "function",
        "inputs": [
            {"name": "oracle", "type": "address"},
            {"name": "questionId", "type": "bytes32"},
            {"name": "outcomeSlotCount", "type": "uint256"},
        ],
        "outputs": [{"type": "bytes32"}],
        "stateMutability": "pure",
    },
    {
        "name": "getCollectionId",
        "type": "function",
        "inputs": [
            {"name": "parentCollectionId", "type": "bytes32"},
            {"name": "conditionId", "type": "bytes32"},
            {"name": "indexSet", "type": "uint256"},
        ],
        "outputs": [{"type": "bytes32"}],
        "stateMutability": "view",
    },
    {
        "name": "getPositionId",
        "type": "function",
        "inputs": [
            {"name": "collateralToken", "type": "address"},
            {"name": "collectionId", "type": "bytes32"},
        ],
        "outputs": [{"type": "uint256"}],
        "stateMutability": "pure",
    },
    {
        "name": "balanceOf",
        "type": "function",
        "inputs": [
            {"name": "owner", "type": "address"},
            {"name": "id", "type": "uint256"},
        ],
        "outputs": [{"type": "uint256"}],
        "stateMutability": "view",
    },
]


def redeem(token_id_hex: str, outcome: str) -> dict:
    rpc_url     = os.getenv("POLYGON_RPC_URL")
    private_key = os.getenv("POLYMARKET_PRIVATE_KEY", "")
    if private_key.startswith("0x"):
        private_key = private_key[2:]

    proxy_address = os.getenv("POLYMARKET_PROXY_ADDRESS", "")

    web3 = Web3(Web3.HTTPProvider(rpc_url))
    if not web3.is_connected():
        return {"success": False, "error": "Cannot connect to Polygon RPC"}

    account = Account.from_key(private_key)
    eoa     = account.address
    holder  = Web3.to_checksum_address(proxy_address) if proxy_address else eoa

    ctf = web3.eth.contract(address=CTF_ADDRESS, abi=CTF_ABI)

    # token_id is the ERC-1155 position ID (decimal integer as hex string from Polymarket)
    try:
        position_id = int(token_id_hex, 16) if token_id_hex.startswith("0x") else int(token_id_hex)
    except ValueError:
        return {"success": False, "error": f"Invalid token_id: {token_id_hex}"}

    # Check balance
    balance = ctf.functions.balanceOf(holder, position_id).call()
    if balance == 0:
        return {"success": False, "error": f"No balance for token {token_id_hex} in wallet {holder}"}

    # Derive condition_id from position_id.
    # Polymarket token IDs encode: keccak256(abi.encode(collateralToken, collectionId))
    # We need the condition_id to call redeemPositions.
    # The simplest approach: use the Polymarket Gamma API to get conditionId for this token.
    import urllib.request
    gamma_url = f"https://gamma-api.polymarket.com/markets?clob_token_ids={token_id_hex}"
    try:
        resp = urllib.request.urlopen(gamma_url, timeout=10).read().decode()
        markets = json.loads(resp)
        if not markets:
            return {"success": False, "error": "Market not found via Gamma API"}
        market = markets[0]
        condition_id_hex = market.get("conditionId") or market.get("condition_id")
        if not condition_id_hex:
            return {"success": False, "error": "conditionId not in Gamma response"}
    except Exception as e:
        return {"success": False, "error": f"Gamma API error: {e}"}

    condition_id = bytes.fromhex(condition_id_hex.replace("0x", ""))

    # index_set: YES=1 (index 0, bit 0), NO=2 (index 1, bit 1)
    index_set = 1 if outcome.upper() == "YES" else 2

    # Build and send transaction from EOA
    nonce = web3.eth.get_transaction_count(eoa)
    gas_price = web3.eth.gas_price

    try:
        tx = ctf.functions.redeemPositions(
            USDC_ADDRESS,
            ZERO_BYTES32,
            condition_id,
            [index_set],
        ).build_transaction({
            "chainId": 137,
            "from": eoa,
            "nonce": nonce,
            "gas": 200000,
            "gasPrice": gas_price,
        })

        signed = account.sign_transaction(tx)
        tx_hash = web3.eth.send_raw_transaction(signed.raw_transaction)
        receipt = web3.eth.wait_for_transaction_receipt(tx_hash, timeout=120)

        if receipt.status == 1:
            return {
                "success": True,
                "tx_hash": tx_hash.hex(),
                "balance_redeemed": balance,
            }
        else:
            return {"success": False, "error": f"Transaction reverted: {tx_hash.hex()}"}

    except Exception as e:
        return {"success": False, "error": str(e)}


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(json.dumps({"success": False, "error": "Usage: redeem_position.py <json>"}))
        sys.exit(1)

    try:
        data = json.loads(sys.argv[1])
        token_id = data["token_id"]
        outcome  = data.get("outcome", "YES")
    except Exception as e:
        print(json.dumps({"success": False, "error": f"Invalid input: {e}"}))
        sys.exit(1)

    result = redeem(token_id, outcome)
    print(json.dumps(result))
    sys.exit(0 if result["success"] else 1)
