#!/usr/bin/env python3
"""
Approve USDC for CTF Exchange
This allows the Polymarket exchange to use your USDC for trading
"""
import os
from web3 import Web3
from eth_account import Account
from dotenv import load_dotenv

load_dotenv()

# Configuration
rpc_url = os.getenv("POLYGON_RPC_URL")
private_key = os.getenv("POLYMARKET_PRIVATE_KEY")
if private_key.startswith('0x'):
    private_key = private_key[2:]

# Connect to Polygon
web3 = Web3(Web3.HTTPProvider(rpc_url))
if not web3.is_connected():
    print("[ERROR] Failed to connect to Polygon RPC")
    exit(1)

# Derive EOA address
account = Account.from_key(private_key)
eoa_address = account.address

# Addresses
usdc_address = web3.to_checksum_address("0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174")
ctf_exchange = web3.to_checksum_address("0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E")

print("Approve USDC for CTF Exchange")
print("=" * 60)
print(f"EOA Wallet: {eoa_address}")
print(f"USDC Token: {usdc_address}")
print(f"Spender:    {ctf_exchange}")
print()

# USDC contract ABI
usdc_abi = [
    {
        "constant": True,
        "inputs": [{"name": "_owner", "type": "address"}],
        "name": "balanceOf",
        "outputs": [{"name": "balance", "type": "uint256"}],
        "type": "function"
    },
    {
        "constant": True,
        "inputs": [
            {"name": "owner", "type": "address"},
            {"name": "spender", "type": "address"}
        ],
        "name": "allowance",
        "outputs": [{"name": "", "type": "uint256"}],
        "type": "function"
    },
    {
        "constant": False,
        "inputs": [
            {"name": "spender", "type": "address"},
            {"name": "value", "type": "uint256"}
        ],
        "name": "approve",
        "outputs": [{"name": "", "type": "bool"}],
        "type": "function"
    }
]

usdc_contract = web3.eth.contract(address=usdc_address, abi=usdc_abi)

# Check current state
usdc_balance = usdc_contract.functions.balanceOf(eoa_address).call()
matic_balance = web3.eth.get_balance(eoa_address)
allowance = usdc_contract.functions.allowance(eoa_address, ctf_exchange).call()

print(f"USDC Balance: ${usdc_balance / 1e6:.2f}")
print(f"MATIC Balance: {web3.from_wei(matic_balance, 'ether'):.4f} MATIC")
print(f"Current Allowance: ${allowance / 1e6:.2f}")
print()

if usdc_balance == 0:
    print("[ERROR] No USDC in EOA wallet!")
    print(f"Transfer USDC to {eoa_address} first")
    exit(1)

if matic_balance == 0:
    print("[ERROR] No MATIC for gas fees!")
    print(f"Send ~0.1 MATIC to {eoa_address} first")
    exit(1)

if allowance >= 1000 * 1e6:
    print("[OK] USDC already approved!")
    print(f"Current allowance: ${allowance / 1e6:.2f}")
    exit(0)

# Approve unlimited USDC (standard practice for DEXs)
print("Approving unlimited USDC for CTF Exchange...")
approve_amount = 2**256 - 1  # Max uint256 (unlimited)

# Build transaction
nonce = web3.eth.get_transaction_count(eoa_address)
approve_txn = usdc_contract.functions.approve(ctf_exchange, approve_amount).build_transaction({
    'chainId': 137,  # Polygon
    'gas': 100000,
    'gasPrice': web3.eth.gas_price,
    'nonce': nonce,
})

# Sign transaction
signed_txn = account.sign_transaction(approve_txn)

# Send transaction
print("Sending approval transaction...")
tx_hash = web3.eth.send_raw_transaction(signed_txn.raw_transaction)
print(f"Transaction hash: {tx_hash.hex()}")
print("Waiting for confirmation...")

# Wait for receipt
receipt = web3.eth.wait_for_transaction_receipt(tx_hash, timeout=120)

if receipt.status == 1:
    print()
    print("[SUCCESS] USDC approved for trading!")
    print(f"Transaction: https://polygonscan.com/tx/{tx_hash.hex()}")
    print()
    print("You can now run the bot and it will be able to place trades!")
else:
    print()
    print("[ERROR] Transaction failed!")
    print(f"Check transaction: https://polygonscan.com/tx/{tx_hash.hex()}")
    exit(1)
