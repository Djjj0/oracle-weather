#!/usr/bin/env python3
"""
Verify that wallet is ready for trading
Checks: USDC balance, MATIC balance, USDC approval
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

print("Wallet Readiness Check")
print("=" * 60)
print(f"EOA Wallet: {eoa_address}")
print()

# USDC contract ABI
usdc_abi = [
    {"constant": True, "inputs": [{"name": "_owner", "type": "address"}], "name": "balanceOf", "outputs": [{"name": "balance", "type": "uint256"}], "type": "function"},
    {"constant": True, "inputs": [{"name": "owner", "type": "address"}, {"name": "spender", "type": "address"}], "name": "allowance", "outputs": [{"name": "", "type": "uint256"}], "type": "function"}
]

usdc_contract = web3.eth.contract(address=usdc_address, abi=usdc_abi)

# Check balances
usdc_balance = usdc_contract.functions.balanceOf(eoa_address).call()
matic_balance = web3.eth.get_balance(eoa_address)
allowance = usdc_contract.functions.allowance(eoa_address, ctf_exchange).call()

print(f"USDC Balance:  ${usdc_balance / 1e6:.2f}")
print(f"MATIC Balance: {web3.from_wei(matic_balance, 'ether'):.4f} MATIC")
print(f"USDC Allowance: ${allowance / 1e6:.2f}")
print()

# Check readiness
ready = True

if matic_balance == 0:
    print("[X] MATIC: Need gas for transactions")
    print(f"    Send 0.1 MATIC to {eoa_address}")
    ready = False
else:
    print("[OK] MATIC: Sufficient for gas fees")

if usdc_balance == 0:
    print("[X] USDC: No funds for trading")
    print(f"    Transfer USDC to {eoa_address}")
    ready = False
elif usdc_balance < 20 * 1e6:
    print(f"[!] USDC: Low balance (${usdc_balance / 1e6:.2f})")
    print("    Recommended: $20+ for trading")
    ready = False
else:
    print(f"[OK] USDC: ${usdc_balance / 1e6:.2f} available")

if allowance == 0:
    print("[X] Approval: USDC not approved")
    print("    Run: python scripts/approve_usdc.py")
    ready = False
elif allowance < 1000 * 1e6:
    print(f"[!] Approval: Low allowance (${allowance / 1e6:.2f})")
    print("    Run: python scripts/approve_usdc.py")
    ready = False
else:
    print("[OK] Approval: USDC approved for trading")

print()
print("=" * 60)

if ready:
    print("[SUCCESS] Wallet is ready for trading!")
    print()
    print("You can now run the bot:")
    print("  cd C:\\Users\\djbro\\.local\\bin\\polymarker")
    print("  .\\polymarker.exe")
else:
    print("[NOT READY] Complete the steps above first")

exit(0 if ready else 1)
