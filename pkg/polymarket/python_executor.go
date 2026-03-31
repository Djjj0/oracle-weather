package polymarket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/djbro/oracle-weather/pkg/utils"
)

// PythonOrderRequest represents the input to the Python order script
type PythonOrderRequest struct {
	TokenID string  `json:"token_id"`
	Price   float64 `json:"price"`
	Size    float64 `json:"size"`
	Side    string  `json:"side"`
}

// PythonOrderResponse represents the output from the Python order script
type PythonOrderResponse struct {
	Success   bool                   `json:"success"`
	OrderID   string                 `json:"order_id,omitempty"`
	Status    string                 `json:"status,omitempty"`
	Error     string                 `json:"error,omitempty"`
	ErrorType string                 `json:"error_type,omitempty"`
	Response  map[string]interface{} `json:"response,omitempty"`
}

// PlaceOrderViaPython places an order using the official Python SDK
// This provides reliable order placement with proper signature handling
func PlaceOrderViaPython(ctx context.Context, tokenID string, price, size float64, side string) (*PythonOrderResponse, error) {
	// Prepare order request
	orderReq := PythonOrderRequest{
		TokenID: tokenID,
		Price:   price,
		Size:    size,
		Side:    side,
	}

	// Marshal to JSON
	reqJSON, err := json.Marshal(orderReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal order request: %w", err)
	}

	// Get Python script path relative to the executable's directory
	exePath, _ := filepath.Abs(filepath.Join("scripts", "place_order.py"))
	scriptPath := exePath

	// Find Python interpreter - use full path to avoid Windows PATH issues
	pythonExe := "python"
	if path, err := exec.LookPath("python"); err == nil {
		pythonExe = path
	} else if path, err := exec.LookPath("python3"); err == nil {
		pythonExe = path
	}

	// Create command with timeout.
	// py_clob_client.create_or_derive_api_creds() makes multiple Polygon RPC calls
	// and can take 20-30s on slow connections. Use 90s to avoid false timeouts.
	cmdCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, pythonExe, scriptPath, string(reqJSON))

	// Force unbuffered Python output so stdout is flushed before exit
	cmd.Env = append(cmd.Environ(), "PYTHONUNBUFFERED=1")

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Log the order attempt
	utils.Logger.Debugf("Calling Python SDK to place order: tokenID=%s, price=%.2f, size=%.2f, side=%s",
		tokenID, price, size, side)

	// Execute command
	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)

	// Parse response
	var response PythonOrderResponse
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
			return nil, fmt.Errorf("failed to parse Python response: %w (output: %s)", err, stdout.String())
		}
	} else {
		// No stdout - log stderr to help diagnose
		if stderr.Len() > 0 {
			utils.Logger.Errorf("Python script stderr: %s", stderr.String())
		}
		if err != nil {
			return nil, fmt.Errorf("Python script execution failed: %w (python=%s, script=%s, stderr=%s)", err, pythonExe, scriptPath, stderr.String())
		}
		return nil, fmt.Errorf("Python script produced no output (python=%s, script=%s, stderr=%s)", pythonExe, scriptPath, stderr.String())
	}

	// Log result
	if response.Success {
		utils.Logger.Infof("✅ Order placed successfully via Python SDK (%.0fms): OrderID=%s, Status=%s",
			duration.Milliseconds(), response.OrderID, response.Status)
	} else {
		utils.Logger.Warnf("Python SDK order failed (%.0fms): %s", duration.Milliseconds(), response.Error)
	}

	return &response, nil
}

// PlaceMarketOrderPython is a convenience wrapper for market orders
func PlaceMarketOrderPython(ctx context.Context, tokenID string, price, size float64) error {
	resp, err := PlaceOrderViaPython(ctx, tokenID, price, size, "BUY")
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("order placement failed: %s", resp.Error)
	}

	return nil
}

// RedeemPositionViaPython redeems a resolved winning position on-chain via
// scripts/redeem_position.py. Input: token ID and outcome ("Yes"/"No").
func RedeemPositionViaPython(ctx context.Context, tokenID, outcome string) (*PythonOrderResponse, error) {
	type redeemRequest struct {
		TokenID string `json:"token_id"`
		Outcome string `json:"outcome"`
	}
	reqJSON, err := json.Marshal(redeemRequest{TokenID: tokenID, Outcome: outcome})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal redeem request: %w", err)
	}

	exePath, _ := filepath.Abs(filepath.Join("scripts", "redeem_position.py"))

	pythonExe := "python"
	if path, err := exec.LookPath("python"); err == nil {
		pythonExe = path
	} else if path, err := exec.LookPath("python3"); err == nil {
		pythonExe = path
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, pythonExe, exePath, string(reqJSON))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	_ = cmd.Run()
	duration := time.Since(start)

	var response PythonOrderResponse
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
			return nil, fmt.Errorf("failed to parse redeem response: %w (output: %s)", err, stdout.String())
		}
	} else {
		if stderr.Len() > 0 {
			utils.Logger.Errorf("Redeem script stderr: %s", stderr.String())
		}
		return nil, fmt.Errorf("redeem script produced no output (python=%s, script=%s)", pythonExe, exePath)
	}

	if response.Success {
		utils.Logger.Infof("✅ Position redeemed via Python (%.0fms): token=%s outcome=%s", duration.Milliseconds(), tokenID, outcome)
	} else {
		utils.Logger.Warnf("Redeem failed (%.0fms): %s", duration.Milliseconds(), response.Error)
	}

	return &response, nil
}

// PlaceSellOrderPython is a convenience wrapper for sell orders
func PlaceSellOrderPython(ctx context.Context, tokenID string, price, size float64) error {
	resp, err := PlaceOrderViaPython(ctx, tokenID, price, size, "SELL")
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("order placement failed: %s", resp.Error)
	}

	return nil
}
