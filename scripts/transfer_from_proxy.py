#!/usr/bin/env python3
"""
Transfer USDC from Magic wallet proxy to EOA wallet
This allows trading with signature_type=0 which produces valid signatures
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
    print("❌ Failed to connect to Polygon RPC")
    exit(1)

# Derive EOA address from private key
account = Account.from_key(private_key)
eoa_address = account.address

# Addresses (checksummed)
proxy_address = web3.to_checksum_address("0x6ff7ae88dbba1834f7647f4153fe30897904931d")
usdc_address = web3.to_checksum_address("0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174")

print("Transfer USDC from Proxy to EOA")
print("=" * 60)
print(f"From (Proxy): {proxy_address}")
print(f"To (EOA):     {eoa_address}")
print()

# USDC contract ABI (minimal)
usdc_abi = [
    {
        "constant": True,
        "inputs": [{"name": "_owner", "type": "address"}],
        "name": "balanceOf",
        "outputs": [{"name": "balance", "type": "uint256"}],
        "type": "function"
    },
    {
        "constant": False,
        "inputs": [
            {"name": "_to", "type": "address"},
            {"name": "_value", "type": "uint256"}
        ],
        "name": "transfer",
        "outputs": [{"name": "", "type": "bool"}],
        "type": "function"
    }
]

usdc_contract = web3.eth.contract(address=usdc_address, abi=usdc_abi)

# Check balances
proxy_balance = usdc_contract.functions.balanceOf(proxy_address).call()
eoa_balance = usdc_contract.functions.balanceOf(eoa_address).call()
matic_balance = web3.eth.get_balance(eoa_address)

print(f"Proxy USDC Balance: ${proxy_balance / 1e6:.2f}")
print(f"EOA USDC Balance:   ${eoa_balance / 1e6:.2f}")
print(f"EOA MATIC Balance:  {web3.from_wei(matic_balance, 'ether'):.4f} MATIC")
print()

if proxy_balance == 0:
    print("[ERROR] No USDC in proxy wallet to transfer")
    exit(1)

if matic_balance == 0:
    print("[ERROR] No MATIC in EOA wallet for gas fees!")
    print()
    print("You need to:")
    print("1. Send some MATIC (0.1 MATIC = ~$0.10) to your EOA wallet:")
    print(f"   {eoa_address}")
    print("2. Then run this script again to transfer USDC")
    exit(1)

# Ask for confirmation
print("[WARNING] This script CANNOT transfer from proxy wallet directly!")
print()
print("Magic wallet proxies require special contract calls that only Polymarket")
print("dashboard can make. You have two options:")
print()
print("OPTION 1 (Recommended): Use Polymarket Dashboard")
print("  1. Go to polymarket.com")
print("  2. Click 'Wallet' -> 'Withdraw'")
print(f"  3. Send $100 USDC to: {eoa_address}")
print("  4. Then the bot will work with signature_type=0")
print()
print("OPTION 2: Contact Polymarket support")
print("  Ask them how to transfer from Magic wallet proxy to EOA")
print()
print("=" * 60)
print()
print("Once you transfer USDC to the EOA wallet, the bot will work!")
print(f"EOA Address: {eoa_address}")
