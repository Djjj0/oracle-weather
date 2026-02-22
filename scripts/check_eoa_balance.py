#!/usr/bin/env python3
"""Check EOA wallet balance and approval"""
import os
from web3 import Web3
from dotenv import load_dotenv

load_dotenv()

# Connect to Polygon
rpc_url = os.getenv("POLYGON_RPC_URL")
web3 = Web3(Web3.HTTPProvider(rpc_url))

# Addresses
private_key = os.getenv("POLYMARKET_PRIVATE_KEY")
if private_key.startswith('0x'):
    private_key = private_key[2:]

from eth_account import Account
account = Account.from_key(private_key)
eoa_address = account.address

usdc_address = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"
ctf_exchange = "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E"

print(f"Checking EOA Wallet: {eoa_address}")
print("=" * 60)

# Check MATIC balance
matic_balance = web3.eth.get_balance(eoa_address)
print(f"MATIC Balance: {web3.from_wei(matic_balance, 'ether')} MATIC")

# Check USDC balance
usdc_abi = [{"constant":True,"inputs":[{"name":"_owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"balance","type":"uint256"}],"type":"function"},
            {"constant":True,"inputs":[{"name":"owner","type":"address"},{"name":"spender","type":"address"}],"name":"allowance","outputs":[{"name":"","type":"uint256"}],"type":"function"}]

usdc_contract = web3.eth.contract(address=usdc_address, abi=usdc_abi)

usdc_balance = usdc_contract.functions.balanceOf(eoa_address).call()
print(f"USDC Balance: {usdc_balance / 1e6} USDC")

# Check allowance
allowance = usdc_contract.functions.allowance(eoa_address, ctf_exchange).call()
print(f"USDC Allowance for CTF Exchange: {allowance / 1e6} USDC")

print("=" * 60)

if matic_balance == 0:
    print("⚠ WARNING: No MATIC for gas fees!")

if usdc_balance == 0:
    print("❌ ERROR: No USDC in wallet!")
elif usdc_balance < 20 * 1e6:
    print(f"⚠ WARNING: Low USDC balance (need at least $20 for trades)")

if allowance == 0:
    print("❌ ERROR: USDC not approved for CTF Exchange!")
    print("   Run the approval script to fix this")
elif allowance < 1000 * 1e6:
    print(f"⚠ WARNING: Low allowance ({allowance / 1e6} USDC)")
else:
    print("✅ Wallet is funded and approved!")
