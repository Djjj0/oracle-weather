package polymarket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/djbro/polymarket-oracle-bot/pkg/utils"
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

	// Get Python script path (assuming scripts directory is in project root)
	scriptPath := filepath.Join("scripts", "place_order.py")

	// Create command with timeout
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "python", scriptPath, string(reqJSON))

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
		// No stdout, check if there was an execution error
		if err != nil {
			return nil, fmt.Errorf("Python script execution failed: %w (stderr: %s)", err, stderr.String())
		}
		return nil, fmt.Errorf("Python script produced no output (stderr: %s)", stderr.String())
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
