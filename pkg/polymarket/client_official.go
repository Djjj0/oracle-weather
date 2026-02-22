package polymarket

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/djbro/polymarket-oracle-bot/internal/config"
	"github.com/djbro/polymarket-oracle-bot/pkg/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-resty/resty/v2"
	buildersdk "github.com/polymarket/go-builder-signing-sdk"
	"github.com/polymarket/go-order-utils/pkg/builder"
	"github.com/polymarket/go-order-utils/pkg/model"
	"golang.org/x/time/rate"
)

// PolymarketClientOfficial uses official Polymarket Go packages
type PolymarketClientOfficial struct {
	client        *resty.Client
	gammaClient   *resty.Client
	config        *config.Config
	rateLimiter   *rate.Limiter
	ethClient     *ethclient.Client
	privateKey    *ecdsa.PrivateKey
	walletAddress common.Address
	proxyAddress  common.Address
	chainID       *big.Int
	orderBuilder  builder.ExchangeOrderBuilder
	authSigner    buildersdk.Signer
}

// NewClientOfficial creates a new client using official Polymarket packages
func NewClientOfficial(cfg *config.Config) *PolymarketClientOfficial {
	// CLOB API client
	client := resty.New()
	client.SetBaseURL(cfg.PolymarketBaseURL)
	client.SetTimeout(30 * time.Second)

	// Gamma API client
	gammaClient := resty.New()
	gammaClient.SetBaseURL(cfg.PolymarketGammaURL)
	gammaClient.SetTimeout(30 * time.Second)

	// Rate limiting
	rateLimiter := rate.NewLimiter(rate.Every(100*time.Millisecond), 10)

	// Connect to Polygon RPC
	ethClient, err := ethclient.Dial(cfg.PolygonRPCURL)
	if err != nil {
		utils.Logger.Warnf("Failed to connect to Polygon RPC: %v", err)
		ethClient = nil
	}

	// Parse private key
	privateKeyHex := strings.TrimPrefix(cfg.PolymarketPrivateKey, "0x")
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		utils.Logger.Fatalf("Invalid private key: %v", err)
	}

	// Get wallet address from private key
	walletAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	utils.Logger.Infof("EOA wallet (signer): %s", walletAddress.Hex())

	// Magic wallet proxy address (where funds are held)
	proxyAddress := common.HexToAddress("0x6ff7ae88dbba1834f7647f4153fe30897904931d")
	utils.Logger.Infof("Magic wallet proxy (maker): %s", proxyAddress.Hex())

	// Initialize order builder using official go-order-utils
	chainID := big.NewInt(int64(cfg.ChainID))
	orderBuilder := builder.NewExchangeOrderBuilderImpl(chainID, nil)

	// Initialize auth signer using official go-builder-signing-sdk
	var authSigner buildersdk.Signer
	if cfg.PolymarketAPIKey != "" && cfg.PolymarketAPISecret != "" && cfg.PolymarketAPIPassphrase != "" {
		signerConfig := buildersdk.LocalSignerConfig{
			Key:        cfg.PolymarketAPIKey,
			Secret:     cfg.PolymarketAPISecret,
			Passphrase: cfg.PolymarketAPIPassphrase,
		}
		authSigner, err = buildersdk.NewLocalSigner(signerConfig)
		if err != nil {
			utils.Logger.Warnf("Failed to create auth signer: %v", err)
		} else {
			utils.Logger.Info("Using API credentials from .env file")
		}
	} else {
		utils.Logger.Warn("No API credentials provided - some operations may fail")
	}

	c := &PolymarketClientOfficial{
		client:        client,
		gammaClient:   gammaClient,
		config:        cfg,
		rateLimiter:   rateLimiter,
		ethClient:     ethClient,
		privateKey:    privateKey,
		walletAddress: walletAddress,
		proxyAddress:  proxyAddress,
		chainID:       chainID,
		orderBuilder:  orderBuilder,
		authSigner:    authSigner,
	}

	return c
}

// addAuthHeaders adds HMAC authentication headers for CLOB API using official SDK
func (c *PolymarketClientOfficial) addAuthHeaders(req *resty.Request, method, path, body string) error {
	if c.authSigner == nil {
		return fmt.Errorf("auth signer not initialized")
	}

	// Use official go-builder-signing-sdk to create authentication headers
	headers, err := c.authSigner.CreateHeaders(method, path, &body, nil)
	if err != nil {
		return fmt.Errorf("failed to create auth headers: %w", err)
	}

	// Set the headers on the request
	req.SetHeaders(headers)
	req.SetHeader("Content-Type", "application/json")

	return nil
}

// CreateOrder creates and signs an order using official go-order-utils
func (c *PolymarketClientOfficial) CreateOrder(tokenID string, price float64, size float64, side string) (*model.SignedOrder, error) {
	c.rateLimiter.Wait(context.Background())

	// Determine order side
	var orderSide model.Side
	if strings.ToUpper(side) == "BUY" {
		orderSide = model.BUY
	} else if strings.ToUpper(side) == "SELL" {
		orderSide = model.SELL
	} else {
		return nil, fmt.Errorf("invalid side: %s (must be BUY or SELL)", side)
	}

	// Calculate maker and taker amounts
	// For BUY orders: we're buying outcome tokens with USDC
	// - MakerAmount = USDC amount we're paying (size in USDC)
	// - TakerAmount = outcome tokens we receive (size / price)
	// For SELL orders: we're selling outcome tokens for USDC
	// - MakerAmount = outcome tokens we're selling (size / price)
	// - TakerAmount = USDC amount we receive (size in USDC)

	// Convert to 6 decimal places (Polymarket uses 6 decimals for amounts)
	var makerAmount, takerAmount *big.Int
	if orderSide == model.BUY {
		// Buying: pay USDC, receive tokens
		makerAmount = new(big.Int).SetInt64(int64(size * 1000000))              // USDC amount
		takerAmount = new(big.Int).SetInt64(int64((size / price) * 1000000))    // Token amount
	} else {
		// Selling: pay tokens, receive USDC
		makerAmount = new(big.Int).SetInt64(int64((size / price) * 1000000))    // Token amount
		takerAmount = new(big.Int).SetInt64(int64(size * 1000000))              // USDC amount
	}

	// Use proxy address as maker if available, otherwise use wallet address
	makerAddress := c.walletAddress.Hex()
	if c.proxyAddress != (common.Address{}) {
		makerAddress = c.proxyAddress.Hex()
	}

	// Create order data
	orderData := &model.OrderData{
		Maker:         makerAddress,
		Taker:         "0x0000000000000000000000000000000000000000", // Any taker (public order)
		TokenId:       tokenID,
		MakerAmount:   makerAmount.String(),
		TakerAmount:   takerAmount.String(),
		Side:          orderSide,
		FeeRateBps:    "0",                                                      // No maker fee
		Nonce:         strconv.FormatInt(time.Now().Unix(), 10),
		Signer:        c.walletAddress.Hex(),                                     // EOA signs
		Expiration:    strconv.FormatInt(time.Now().Add(30*24*time.Hour).Unix(), 10), // 30 days
		SignatureType: model.POLY_PROXY,                                          // Use proxy signature
	}

	utils.Logger.Debugf("Creating order: tokenID=%s, side=%s, maker=%s, taker=%s, makerAmt=%s, takerAmt=%s",
		tokenID, side, makerAddress, orderData.Taker, orderData.MakerAmount, orderData.TakerAmount)

	// Build and sign the order using official go-order-utils
	signedOrder, err := c.orderBuilder.BuildSignedOrder(
		c.privateKey,
		orderData,
		model.CTFExchange, // Using CTFExchange contract
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build signed order: %w", err)
	}

	utils.Logger.Debugf("Order signed successfully: salt=%s, signature=%x",
		signedOrder.Salt.String(), signedOrder.Signature)

	return signedOrder, nil
}

// PlaceOrder submits a signed order to the CLOB API
func (c *PolymarketClientOfficial) PlaceOrder(signedOrder *model.SignedOrder) error {
	c.rateLimiter.Wait(context.Background())

	// Convert SignedOrder to API format
	orderPayload := map[string]interface{}{
		"order": map[string]interface{}{
			"salt":          signedOrder.Salt.String(),
			"maker":         signedOrder.Maker.Hex(),
			"signer":        signedOrder.Signer.Hex(),
			"taker":         signedOrder.Taker.Hex(),
			"tokenId":       signedOrder.TokenId.String(),
			"makerAmount":   signedOrder.MakerAmount.String(),
			"takerAmount":   signedOrder.TakerAmount.String(),
			"side":          signedOrder.Side.String(),
			"expiration":    signedOrder.Expiration.String(),
			"nonce":         signedOrder.Nonce.String(),
			"feeRateBps":    signedOrder.FeeRateBps.String(),
			"signatureType": signedOrder.SignatureType.String(),
		},
		"signature": fmt.Sprintf("0x%x", signedOrder.Signature),
	}

	orderJSON, err := json.Marshal(orderPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal order: %w", err)
	}

	req := c.client.R().SetBody(orderJSON)

	// Add authentication headers using official SDK
	if err := c.addAuthHeaders(req, "POST", "/order", string(orderJSON)); err != nil {
		return fmt.Errorf("failed to add auth headers: %w", err)
	}

	resp, err := req.Post("/order")
	if err != nil {
		return fmt.Errorf("failed to place order: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("API error: status %d, body: %s", resp.StatusCode(), resp.String())
	}

	utils.Logger.Infof("Order placed successfully: %s", resp.String())
	return nil
}

// GetBalance retrieves the balance and allowance for the wallet
func (c *PolymarketClientOfficial) GetBalance() (float64, error) {
	c.rateLimiter.Wait(context.Background())

	address := c.walletAddress.Hex()
	if c.proxyAddress != (common.Address{}) {
		address = c.proxyAddress.Hex()
	}

	req := c.client.R()
	path := fmt.Sprintf("/balance-allowance?address=%s", address)

	// Add authentication headers
	if err := c.addAuthHeaders(req, "GET", path, ""); err != nil {
		return 0, fmt.Errorf("failed to add auth headers: %w", err)
	}

	resp, err := req.Get(path)
	if err != nil {
		return 0, fmt.Errorf("failed to get balance: %w", err)
	}

	if resp.IsError() {
		return 0, fmt.Errorf("API error: status %d, body: %s", resp.StatusCode(), resp.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return 0, fmt.Errorf("failed to parse balance response: %w", err)
	}

	// Parse balance from response
	if balanceStr, ok := result["balance"].(string); ok {
		balance, err := strconv.ParseFloat(balanceStr, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse balance: %w", err)
		}
		return balance / 1000000, nil // Convert from 6 decimals
	}

	return 0, fmt.Errorf("balance not found in response")
}

// GetActiveMarkets retrieves all active markets from Gamma API (with pagination)
func (c *PolymarketClientOfficial) GetActiveMarkets() ([]Market, error) {
	const pageSize = 500
	var allMarkets []Market

	for offset := 0; ; offset += pageSize {
		c.rateLimiter.Wait(context.Background())

		var page []Market
		resp, err := c.gammaClient.R().
			SetResult(&page).
			SetQueryParam("closed", "false").
			SetQueryParam("limit", fmt.Sprintf("%d", pageSize)).
			SetQueryParam("offset", fmt.Sprintf("%d", offset)).
			Get("/markets")

		if err != nil {
			return nil, fmt.Errorf("failed to get active markets (offset %d): %w", offset, err)
		}

		if resp.IsError() {
			return nil, fmt.Errorf("API error (offset %d): %s", offset, resp.Status())
		}

		if len(page) == 0 {
			break
		}

		allMarkets = append(allMarkets, page...)

		// If we got fewer than pageSize, we've reached the end
		if len(page) < pageSize {
			break
		}
	}

	// Set fetch timestamp and parse JSON string fields for all markets
	fetchTime := time.Now()
	for i := range allMarkets {
		allMarkets[i].FetchedAt = fetchTime

		// Parse outcomes from JSON string
		if allMarkets[i].OutcomesStr != "" {
			allMarkets[i].Outcomes = parseJSONStringArray(allMarkets[i].OutcomesStr)
		}

		// Parse token IDs from JSON string
		if allMarkets[i].TokenIDsStr != "" {
			allMarkets[i].TokenIDs = parseJSONStringArray(allMarkets[i].TokenIDsStr)
		}

		// Parse outcome prices from JSON string to float array
		if allMarkets[i].OutcomePricesStr != "" {
			allMarkets[i].Prices = parseOutcomePrices(allMarkets[i].OutcomePricesStr)
		}
	}

	return allMarkets, nil
}

// parseJSONStringArray parses a JSON array string
func parseJSONStringArray(str string) []string {
	var result []string
	if err := json.Unmarshal([]byte(str), &result); err != nil {
		return []string{}
	}
	return result
}

// parseOutcomePrices parses the outcome prices string from Gamma API
func parseOutcomePrices(pricesStr string) []float64 {
	// Try to parse as JSON array
	var prices []string
	if err := json.Unmarshal([]byte(pricesStr), &prices); err != nil {
		return []float64{}
	}

	// Convert strings to floats
	result := make([]float64, 0, len(prices))
	for _, priceStr := range prices {
		if price, err := strconv.ParseFloat(priceStr, 64); err == nil {
			result = append(result, price)
		}
	}

	return result
}

// GetMarketByID retrieves a specific market by ID
func (c *PolymarketClientOfficial) GetMarketByID(marketID string) (*Market, error) {
	c.rateLimiter.Wait(context.Background())

	resp, err := c.gammaClient.R().
		SetPathParam("id", marketID).
		Get("/markets/{id}")

	if err != nil {
		return nil, fmt.Errorf("failed to get market: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode())
	}

	var market Market
	if err := json.Unmarshal(resp.Body(), &market); err != nil {
		return nil, fmt.Errorf("failed to parse market: %w", err)
	}

	// Parse JSON string fields
	market.FetchedAt = time.Now()
	if market.OutcomesStr != "" {
		market.Outcomes = parseJSONStringArray(market.OutcomesStr)
	}
	if market.TokenIDsStr != "" {
		market.TokenIDs = parseJSONStringArray(market.TokenIDsStr)
	}
	if market.OutcomePricesStr != "" {
		market.Prices = parseOutcomePrices(market.OutcomePricesStr)
	}

	return &market, nil
}

// GetOrderbook retrieves the orderbook for a specific token
func (c *PolymarketClientOfficial) GetOrderbook(tokenID string) (*Orderbook, error) {
	c.rateLimiter.Wait(context.Background())

	resp, err := c.client.R().
		SetQueryParam("token_id", tokenID).
		Get("/book")

	if err != nil {
		return nil, fmt.Errorf("failed to get orderbook: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode())
	}

	var orderbook Orderbook
	if err := json.Unmarshal(resp.Body(), &orderbook); err != nil {
		return nil, fmt.Errorf("failed to parse orderbook: %w", err)
	}

	return &orderbook, nil
}

// PlaceMarketOrder places a market buy order
func (c *PolymarketClientOfficial) PlaceMarketOrder(tokenID string, price, size float64) error {
	signedOrder, err := c.CreateOrder(tokenID, price, size, "BUY")
	if err != nil {
		return fmt.Errorf("failed to create order: %w", err)
	}

	return c.PlaceOrder(signedOrder)
}

// PlaceSellOrder places a market sell order
func (c *PolymarketClientOfficial) PlaceSellOrder(tokenID string, price, size float64) error {
	signedOrder, err := c.CreateOrder(tokenID, price, size, "SELL")
	if err != nil {
		return fmt.Errorf("failed to create order: %w", err)
	}

	return c.PlaceOrder(signedOrder)
}

// GetGasPrice estimates the current gas price on Polygon
func (c *PolymarketClientOfficial) GetGasPrice() (*big.Int, error) {
	if c.ethClient == nil {
		return big.NewInt(50000000000), nil // Default: 50 gwei
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	gasPrice, err := c.ethClient.SuggestGasPrice(ctx)
	if err != nil {
		utils.Logger.Warnf("Failed to get gas price: %v, using default", err)
		return big.NewInt(50000000000), nil // Default: 50 gwei
	}

	return gasPrice, nil
}

// ShouldExecuteTrade determines if a trade should be executed based on gas costs
func (c *PolymarketClientOfficial) ShouldExecuteTrade(expectedProfit, positionSize float64) (bool, string) {
	// Get gas price
	gasPrice, err := c.GetGasPrice()
	if err != nil {
		utils.Logger.Warnf("Failed to get gas price: %v", err)
		gasPrice = big.NewInt(50000000000) // Default: 50 gwei
	}

	// Estimate gas cost in MATIC
	gasLimit := big.NewInt(250000) // Estimated gas limit for trade
	gasCostWei := new(big.Int).Mul(gasPrice, gasLimit)
	gasCostMATIC := new(big.Float).SetInt(gasCostWei)
	gasCostMATIC.Quo(gasCostMATIC, big.NewFloat(1e18))

	// Convert MATIC to USD (approximate: 1 MATIC ≈ $0.80)
	maticPriceUSD := 0.80
	gasCostUSD, _ := gasCostMATIC.Float64()
	gasCostUSD *= maticPriceUSD

	// Add Polymarket trading fees (0.15% on maker side, 3% on taker side - worst case 3%)
	tradingFees := positionSize * 0.03

	// Total cost
	totalCost := gasCostUSD + tradingFees

	// expectedProfit is a percentage (decimal), so multiply by positionSize to get absolute profit
	absoluteProfit := expectedProfit * positionSize

	// Net profit after costs
	netProfit := absoluteProfit - totalCost

	if netProfit <= 0 {
		return false, fmt.Sprintf("costs (gas: $%.4f, fees: $%.4f = $%.4f) exceed profit ($%.2f)",
			gasCostUSD, tradingFees, totalCost, absoluteProfit)
	}

	utils.Logger.Debugf("Trade profitable: profit=$%.2f, costs=$%.4f, net=$%.2f",
		absoluteProfit, totalCost, netProfit)

	return true, ""
}

// CreateSplitPosition creates a split position (TODO: implement)
func (c *PolymarketClientOfficial) CreateSplitPosition(marketID string, amount float64) error {
	return fmt.Errorf("CreateSplitPosition not yet implemented in official client")
}

// MergeSplitPosition merges a split position (TODO: implement)
func (c *PolymarketClientOfficial) MergeSplitPosition(marketID string, amount float64) error {
	return fmt.Errorf("MergeSplitPosition not yet implemented in official client")
}

// CheckBalanceAndAllowance checks balance and allowance (for compatibility)
func (c *PolymarketClientOfficial) CheckBalanceAndAllowance() {
	balance, err := c.GetBalance()
	if err != nil {
		utils.Logger.Warnf("Failed to check balance: %v", err)
		return
	}
	utils.Logger.Infof("Wallet balance: $%.2f USDC", balance)
}

// EnsureAllowance ensures the contract has sufficient allowance (TODO: implement)
func (c *PolymarketClientOfficial) EnsureAllowance() error {
	// For now, assume allowance is set
	// In the future, implement on-chain allowance check and approval
	utils.Logger.Info("Allowance check skipped (assumed sufficient)")
	return nil
}
