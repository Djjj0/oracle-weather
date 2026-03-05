package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-resty/resty/v2"
	"github.com/lixvyang/polymarket-sdk-go/auth"
	"github.com/lixvyang/polymarket-sdk-go/client"
	"github.com/lixvyang/polymarket-sdk-go/types"
	buildersdk "github.com/polymarket/go-builder-signing-sdk"
	"github.com/polymarket/go-order-utils/pkg/builder"
	"github.com/polymarket/go-order-utils/pkg/model"
	"golang.org/x/time/rate"
)

// PolymarketClientLixv uses lixvyang SDK for AUTH + manual HTTP for orders
type PolymarketClientLixv struct {
	clobClient    *client.ClobClient
	gammaClient   *resty.Client
	httpClient    *resty.Client
	config        *config.Config
	rateLimiter   *rate.Limiter
	ethClient     *ethclient.Client
	walletAddress common.Address
	proxyAddress  common.Address
	apiCreds      *types.ApiKeyCreds
	authSigner    buildersdk.Signer
	orderBuilder  builder.ExchangeOrderBuilder
	chainID       *big.Int
}

// NewClientLixv creates a new client using lixvyang SDK for authentication
func NewClientLixv(cfg *config.Config) *PolymarketClientLixv {
	utils.Logger.Info("Initializing Polymarket client with lixvyang SDK (for auth)...")

	// Parse private key
	privateKeyHex := cfg.PolymarketPrivateKey
	if len(privateKeyHex) > 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		utils.Logger.Fatalf("Invalid private key: %v", err)
	}

	walletAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	utils.Logger.Info("EOA wallet initialized")

	// Magic wallet proxy address (from Polymarket dashboard)
	proxyAddress := common.HexToAddress("0x6ff7ae88dbba1834f7647f4153fe30897904931d")
	utils.Logger.Info("Magic wallet proxy initialized")

	// Connect to Polygon RPC
	ethClient, err := ethclient.Dial(cfg.PolygonRPCURL)
	if err != nil {
		utils.Logger.Warnf("Failed to connect to Polygon RPC: %v", err)
		ethClient = nil
	}

	// Rate limiting
	rateLimiter := rate.NewLimiter(rate.Every(100*time.Millisecond), 10)

	// Gamma API client
	gammaClient := resty.New()
	gammaClient.SetBaseURL(cfg.PolymarketGammaURL)
	gammaClient.SetTimeout(30 * time.Second)

	// HTTP client for CLOB API
	httpClient := resty.New()
	httpClient.SetBaseURL(cfg.PolymarketBaseURL)
	httpClient.SetTimeout(30 * time.Second)

	// Create CLOB client configuration
	clobConfig := &client.ClientConfig{
		Host:          cfg.PolymarketBaseURL,
		ChainID:       types.ChainPolygon,
		PrivateKey:    cfg.PolymarketPrivateKey,
		APIKey:        nil,
		UseServerTime: true,
		Timeout:       30 * time.Second,
	}

	// Create CLOB client
	clobClient, err := client.NewClobClient(clobConfig)
	if err != nil {
		utils.Logger.Fatalf("Failed to create CLOB client: %v", err)
	}

	utils.Logger.Info("CLOB client created successfully")

	// For Magic wallets, derive API credentials from private key
	utils.Logger.Info("Deriving API credentials from private key...")
	apiCreds, err := clobClient.DeriveApiKey(nil)

	if err != nil {
		utils.Logger.Warnf("Failed to derive API credentials: %v", err)
		utils.Logger.Info("Attempting to create new API credentials...")
		apiCreds, err = clobClient.CreateApiKey(nil)
		if err != nil {
			utils.Logger.Fatalf("Failed to create API credentials: %v", err)
		}
		utils.Logger.Info("✅ API credentials created successfully")
	} else {
		utils.Logger.Info("✅ API credentials derived successfully")
	}

	// Update client with API credentials
	clobConfig.APIKey = apiCreds
	clobClient, err = client.NewClobClient(clobConfig)
	if err != nil {
		utils.Logger.Fatalf("Failed to recreate client with API credentials: %v", err)
	}
	utils.Logger.Info("✅ CLOB client updated with derived credentials")

	// Create auth signer using official SDK with the credentials
	signerConfig := buildersdk.LocalSignerConfig{
		Key:        apiCreds.Key,
		Secret:     apiCreds.Secret,
		Passphrase: apiCreds.Passphrase,
	}
	authSigner, err := buildersdk.NewLocalSigner(signerConfig)
	if err != nil {
		utils.Logger.Fatalf("Failed to create auth signer: %v", err)
	}

	utils.Logger.Info("✅ Auth signer created with derived credentials")

	// Initialize order builder
	chainID := big.NewInt(int64(cfg.ChainID))
	orderBuilder := builder.NewExchangeOrderBuilderImpl(chainID, nil)

	c := &PolymarketClientLixv{
		clobClient:    clobClient,
		gammaClient:   gammaClient,
		httpClient:    httpClient,
		config:        cfg,
		rateLimiter:   rateLimiter,
		ethClient:     ethClient,
		walletAddress: walletAddress,
		proxyAddress:  proxyAddress,
		apiCreds:      apiCreds,
		authSigner:    authSigner,
		orderBuilder:  orderBuilder,
		chainID:       chainID,
	}

	utils.Logger.Info("✅ Polymarket client initialized successfully with working authentication!")
	return c
}

// addAuthHeaders adds HMAC authentication headers using SDK's auth package
func (c *PolymarketClientLixv) addAuthHeaders(req *resty.Request, method, path, body string) error {
	if c.apiCreds == nil {
		return fmt.Errorf("API credentials not initialized")
	}

	// Parse private key from hex string
	privateKey, err := crypto.HexToECDSA(c.config.PolymarketPrivateKey[2:]) // Remove 0x prefix
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	// Use SDK's CreateL2Headers for correct POLY-* header format
	headerArgs := &types.L2HeaderArgs{
		Method:      method,
		RequestPath: path,
		Body:        body,
	}

	l2Headers, err := auth.CreateL2Headers(privateKey, c.apiCreds, headerArgs, nil)
	if err != nil {
		return fmt.Errorf("failed to create L2 headers: %w", err)
	}

	// POLY_ADDRESS should match the address that owns the API credentials
	// For Magic wallets, credentials are derived from the EOA (signer), not the proxy
	headerMap := map[string]string{
		"POLY_ADDRESS":    strings.ToLower(c.walletAddress.Hex()), // EOA address (owns the API key)
		"POLY_SIGNATURE":  l2Headers.POLYSignature,
		"POLY_TIMESTAMP":  l2Headers.POLYTimestamp,
		"POLY_API_KEY":    l2Headers.POLYAPIKey,
		"POLY_PASSPHRASE": l2Headers.POLYPassphrase,
	}

	// Debug logging (masked for security)
	utils.Logger.Debugf("Auth headers created for %s %s", method, path)

	req.SetHeaders(headerMap)
	req.SetHeader("Content-Type", "application/json")

	// Add chain ID header for Magic wallet compatibility
	req.SetHeader("X-Chain-ID", "137")
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CreateOrder creates and signs an order
func (c *PolymarketClientLixv) CreateOrder(tokenID string, price float64, size float64, side string) (*model.SignedOrder, error) {
	c.rateLimiter.Wait(context.Background())

	// Round price to nearest tick size (0.01 for Polymarket)
	tickSize := 0.01
	price = math.Round(price/tickSize) * tickSize

	// Ensure price is within valid range [0.01, 0.99]
	if price < 0.01 {
		price = 0.01
	} else if price > 0.99 {
		price = 0.99
	}

	utils.Logger.Debugf("Creating order: tokenID=%s, price=%.2f (tick-aligned), size=%.2f, side=%s", tokenID, price, size, side)

	var orderSide model.Side
	if side == "BUY" {
		orderSide = model.BUY
	} else {
		orderSide = model.SELL
	}

	var makerAmount, takerAmount *big.Int
	if orderSide == model.BUY {
		// BUY: Spending USDC (maker) to get shares (taker)
		makerAmount = new(big.Int).SetInt64(int64(size * 1000000))

		// Round taker amount to nearest whole share (tick size = 1 share)
		sharesNeeded := size / price
		takerAmount = new(big.Int).SetInt64(int64(sharesNeeded * 1000000))

		utils.Logger.Debugf("BUY order: makerAmount=%s USDC ($%.2f), takerAmount=%s shares (%.6f shares)",
			makerAmount.String(), size, takerAmount.String(), float64(takerAmount.Int64())/1000000)
	} else {
		// SELL: Spending shares (maker) to get USDC (taker)
		sharesNeeded := size / price
		makerAmount = new(big.Int).SetInt64(int64(sharesNeeded * 1000000))
		takerAmount = new(big.Int).SetInt64(int64(size * 1000000))

		utils.Logger.Debugf("SELL order: makerAmount=%s shares (%.6f), takerAmount=%s USDC ($%.2f)",
			makerAmount.String(), float64(makerAmount.Int64())/1000000, takerAmount.String(), size)
	}

	makerAddress := c.proxyAddress.Hex()
	if c.proxyAddress == (common.Address{}) {
		makerAddress = c.walletAddress.Hex()
	}

	orderData := &model.OrderData{
		Maker:         makerAddress,
		Taker:         "0x0000000000000000000000000000000000000000",
		TokenId:       tokenID,
		MakerAmount:   makerAmount.String(),
		TakerAmount:   takerAmount.String(),
		Side:          orderSide,
		FeeRateBps:    "0",
		Nonce:         strconv.FormatInt(time.Now().Unix(), 10),
		Signer:        c.walletAddress.Hex(),
		Expiration:    "0", // GTC orders must have expiration = 0
		SignatureType: model.POLY_PROXY,
	}

	// Parse private key from config
	privateKeyHex := c.config.PolymarketPrivateKey
	if len(privateKeyHex) > 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}
	privateKey, _ := crypto.HexToECDSA(privateKeyHex)

	signedOrder, err := c.orderBuilder.BuildSignedOrder(
		privateKey,
		orderData,
		model.CTFExchange,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build signed order: %w", err)
	}

	return signedOrder, nil
}

// PlaceOrder submits a signed order
func (c *PolymarketClientLixv) PlaceOrder(signedOrder *model.SignedOrder) error {
	c.rateLimiter.Wait(context.Background())

	// Create order payload matching Polymarket API spec EXACTLY
	// Spec: https://docs.polymarket.com/developers/CLOB/orders/create-order
	// Structure: {"order": {...}, "owner": "api_key", "orderType": "GTC"}

	// Convert salt from big.Int to int64 (API requires integer, not string)
	saltInt, _ := strconv.ParseInt(signedOrder.Salt.String(), 10, 64)

	orderPayload := map[string]interface{}{
		"order": map[string]interface{}{
			"salt":          saltInt, // integer (API spec requires integer type)
			"maker":         strings.ToLower(signedOrder.Maker.Hex()),
			"signer":        strings.ToLower(signedOrder.Signer.Hex()),
			"taker":         strings.ToLower(signedOrder.Taker.Hex()),
			"tokenId":       signedOrder.TokenId.String(),
			"makerAmount":   signedOrder.MakerAmount.String(),
			"takerAmount":   signedOrder.TakerAmount.String(),
			"side":          strconv.FormatInt(signedOrder.Side.Int64(), 10), // string "0" or "1" (API requires string!)
			"feeRateBps":    signedOrder.FeeRateBps.String(),
			"nonce":         signedOrder.Nonce.String(),
			"expiration":    signedOrder.Expiration.String(), // "0" for GTC orders
			"signatureType": int(signedOrder.SignatureType.Int64()), // integer (API spec requires integer type)
			"signature":     fmt.Sprintf("0x%x", signedOrder.Signature),
		},
		"owner":     c.apiCreds.Key, // API key
		"orderType": "GTC",           // Good-til-cancelled
	}

	orderJSON, _ := json.Marshal(orderPayload)

	utils.Logger.Debugf("Order payload: %s", string(orderJSON))

	req := c.httpClient.R().SetBody(orderJSON)

	// Add auth headers
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

	utils.Logger.Infof("✅ Order placed successfully: %s", resp.String())
	return nil
}

// GetBalance retrieves balance
func (c *PolymarketClientLixv) GetBalance() (float64, error) {
	c.rateLimiter.Wait(context.Background())

	// TODO: Implement live balance checking via Polymarket API
	// For now, return user's reported balance
	// Bot will fail gracefully if insufficient funds
	return 21.0, nil
}

// GetActiveMarkets retrieves all active markets
func (c *PolymarketClientLixv) GetActiveMarkets() ([]Market, error) {
	return c.getMarkets("false")
}

// GetClosedMarkets retrieves all closed/resolved markets
func (c *PolymarketClientLixv) GetClosedMarkets() ([]Market, error) {
	return c.getMarkets("true")
}

// GetRecentClosedMarkets retrieves recent closed markets (limited to first 2000)
func (c *PolymarketClientLixv) GetRecentClosedMarkets(maxCount int) ([]Market, error) {
	return c.getLimitedMarkets("true", maxCount)
}

// getLimitedMarkets retrieves markets with a maximum count limit
func (c *PolymarketClientLixv) getLimitedMarkets(closedStatus string, maxCount int) ([]Market, error) {
	const pageSize = 500
	var allMarkets []Market

	for offset := 0; len(allMarkets) < maxCount; offset += pageSize {
		c.rateLimiter.Wait(context.Background())

		var page []Market
		resp, err := c.gammaClient.R().
			SetResult(&page).
			SetQueryParam("closed", closedStatus).
			SetQueryParam("limit", fmt.Sprintf("%d", pageSize)).
			SetQueryParam("offset", fmt.Sprintf("%d", offset)).
			Get("/markets")

		if err != nil {
			return nil, fmt.Errorf("failed to get markets (offset %d): %w", offset, err)
		}

		if resp.IsError() {
			return nil, fmt.Errorf("API error (offset %d): %s", offset, resp.Status())
		}

		if len(page) == 0 {
			break
		}

		allMarkets = append(allMarkets, page...)

		// Stop if we've reached the limit
		if len(allMarkets) >= maxCount {
			allMarkets = allMarkets[:maxCount]
			break
		}

		if len(page) < pageSize {
			break
		}
	}

	// Parse JSON fields
	fetchTime := time.Now()
	for i := range allMarkets {
		allMarkets[i].FetchedAt = fetchTime
		if allMarkets[i].OutcomesStr != "" {
			allMarkets[i].Outcomes = parseJSONStringArray(allMarkets[i].OutcomesStr)
		}
		if allMarkets[i].TokenIDsStr != "" {
			allMarkets[i].TokenIDs = parseJSONStringArray(allMarkets[i].TokenIDsStr)
		}
		if allMarkets[i].OutcomePricesStr != "" {
			allMarkets[i].Prices = parseOutcomePrices(allMarkets[i].OutcomePricesStr)
		}
	}

	return allMarkets, nil
}

// getMarkets retrieves markets with specified closed status
func (c *PolymarketClientLixv) getMarkets(closedStatus string) ([]Market, error) {
	const pageSize = 500
	var allMarkets []Market

	for offset := 0; ; offset += pageSize {
		c.rateLimiter.Wait(context.Background())

		var page []Market
		resp, err := c.gammaClient.R().
			SetResult(&page).
			SetQueryParam("closed", closedStatus).
			SetQueryParam("limit", fmt.Sprintf("%d", pageSize)).
			SetQueryParam("offset", fmt.Sprintf("%d", offset)).
			Get("/markets")

		if err != nil {
			return nil, fmt.Errorf("failed to get markets (offset %d): %w", offset, err)
		}

		if resp.IsError() {
			return nil, fmt.Errorf("API error (offset %d): %s", offset, resp.Status())
		}

		if len(page) == 0 {
			break
		}

		allMarkets = append(allMarkets, page...)

		if len(page) < pageSize {
			break
		}
	}

	// Parse JSON fields
	fetchTime := time.Now()
	for i := range allMarkets {
		allMarkets[i].FetchedAt = fetchTime
		if allMarkets[i].OutcomesStr != "" {
			allMarkets[i].Outcomes = parseJSONStringArray(allMarkets[i].OutcomesStr)
		}
		if allMarkets[i].TokenIDsStr != "" {
			allMarkets[i].TokenIDs = parseJSONStringArray(allMarkets[i].TokenIDsStr)
		}
		if allMarkets[i].OutcomePricesStr != "" {
			allMarkets[i].Prices = parseOutcomePrices(allMarkets[i].OutcomePricesStr)
		}
	}

	return allMarkets, nil
}

// GetMarketByID retrieves a specific market
func (c *PolymarketClientLixv) GetMarketByID(marketID string) (*Market, error) {
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

// GetOrderbook retrieves orderbook
func (c *PolymarketClientLixv) GetOrderbook(tokenID string) (*Orderbook, error) {
	c.rateLimiter.Wait(context.Background())

	sdkOrderbook, err := c.clobClient.GetOrderBook(tokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to get orderbook: %w", err)
	}

	orderbook := &Orderbook{
		MarketID: tokenID,
		Bids:     make([]Order, 0),
		Asks:     make([]Order, 0),
	}

	for _, bid := range sdkOrderbook.Bids {
		price, _ := strconv.ParseFloat(bid.Price, 64)
		size, _ := strconv.ParseFloat(bid.Size, 64)
		orderbook.Bids = append(orderbook.Bids, Order{
			Price: price,
			Size:  size,
			Side:  "BUY",
		})
	}

	for _, ask := range sdkOrderbook.Asks {
		price, _ := strconv.ParseFloat(ask.Price, 64)
		size, _ := strconv.ParseFloat(ask.Size, 64)
		orderbook.Asks = append(orderbook.Asks, Order{
			Price: price,
			Size:  size,
			Side:  "SELL",
		})
	}

	return orderbook, nil
}

// PlaceMarketOrder places a market buy order using Python SDK
func (c *PolymarketClientLixv) PlaceMarketOrder(tokenID string, price, size float64) error {
	return PlaceMarketOrderPython(context.Background(), tokenID, price, size)
}

// PlaceSellOrder places a sell order using Python SDK
func (c *PolymarketClientLixv) PlaceSellOrder(tokenID string, price, size float64) error {
	return PlaceSellOrderPython(context.Background(), tokenID, price, size)
}

// GetGasPrice estimates gas price
func (c *PolymarketClientLixv) GetGasPrice() (*big.Int, error) {
	if c.ethClient == nil {
		return big.NewInt(50000000000), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	gasPrice, err := c.ethClient.SuggestGasPrice(ctx)
	if err != nil {
		return big.NewInt(50000000000), nil
	}

	return gasPrice, nil
}

// ShouldExecuteTrade determines if trade is profitable
func (c *PolymarketClientLixv) ShouldExecuteTrade(expectedProfit, positionSize float64) (bool, string) {
	gasPrice, _ := c.GetGasPrice()
	gasLimit := big.NewInt(250000)
	gasCostWei := new(big.Int).Mul(gasPrice, gasLimit)
	gasCostMATIC := new(big.Float).SetInt(gasCostWei)
	gasCostMATIC.Quo(gasCostMATIC, big.NewFloat(1e18))

	maticPriceUSD := 0.80
	gasCostUSD, _ := gasCostMATIC.Float64()
	gasCostUSD *= maticPriceUSD

	tradingFees := positionSize * 0.03
	totalCost := gasCostUSD + tradingFees

	// expectedProfit is a percentage (decimal), so multiply by positionSize to get absolute profit
	absoluteProfit := expectedProfit * positionSize
	netProfit := absoluteProfit - totalCost

	if netProfit <= 0 {
		return false, fmt.Sprintf("costs (gas: $%.4f, fees: $%.4f = $%.4f) exceed profit ($%.2f)",
			gasCostUSD, tradingFees, totalCost, absoluteProfit)
	}

	return true, ""
}

// CheckBalanceAndAllowance compatibility method
func (c *PolymarketClientLixv) CheckBalanceAndAllowance() {
	_, err := c.clobClient.GetClosedOnlyMode()
	if err != nil {
		utils.Logger.Warnf("Account check failed: %v", err)
	} else {
		utils.Logger.Info("✅ Account status check successful - auth working!")
	}
}

// EnsureAllowance compatibility method
func (c *PolymarketClientLixv) EnsureAllowance() error {
	return nil
}

// CreateSplitPosition not implemented
func (c *PolymarketClientLixv) CreateSplitPosition(marketID string, amount float64) error {
	return fmt.Errorf("not implemented")
}

// MergeSplitPosition not implemented
func (c *PolymarketClientLixv) MergeSplitPosition(marketID string, amount float64) error {
	return fmt.Errorf("not implemented")
}
