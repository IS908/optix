package analysis

import (
	"context"
	"fmt"

	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
	marketdatav1 "github.com/IS908/optix/gen/go/optix/marketdata/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// defaultCallOpts are applied to every RPC call. WaitForReady makes the client
// block until the server is ready rather than failing immediately with
// UNAVAILABLE, which eliminates startup race conditions.
var defaultCallOpts = []grpc.CallOption{grpc.WaitForReady(true)}

// Client wraps the gRPC connection to the Python analysis engine.
type Client struct {
	conn   *grpc.ClientConn
	svc    analysisv1.AnalysisServiceClient
}

// NewClient connects to the Python analysis gRPC server.
func NewClient(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect to analysis engine at %s: %w", addr, err)
	}
	return &Client{
		conn: conn,
		svc:  analysisv1.NewAnalysisServiceClient(conn),
	}, nil
}

// Close closes the gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// PriceOptionResult holds the result of a single option pricing call.
type PriceOptionResult struct {
	Price  float64
	Delta  float64
	Gamma  float64
	Theta  float64
	Vega   float64
	Rho    float64
}

// PriceOption calls the Python Black-Scholes pricing engine.
func (c *Client) PriceOption(ctx context.Context,
	spotPrice, strike, timeToExpiry, riskFreeRate, volatility, dividendYield float64,
	optionType string,
) (*PriceOptionResult, error) {
	ot := marketdatav1.OptionType_OPTION_TYPE_CALL
	if optionType == "put" {
		ot = marketdatav1.OptionType_OPTION_TYPE_PUT
	}

	resp, err := c.svc.PriceOption(ctx, &analysisv1.PriceOptionRequest{
		SpotPrice:     spotPrice,
		Strike:        strike,
		TimeToExpiry:  timeToExpiry,
		RiskFreeRate:  riskFreeRate,
		Volatility:    volatility,
		DividendYield: dividendYield,
		OptionType:    ot,
	}, defaultCallOpts...)
	if err != nil {
		return nil, fmt.Errorf("PriceOption: %w", err)
	}

	result := &PriceOptionResult{Price: resp.Price}
	if g := resp.Greeks; g != nil {
		result.Delta = g.Delta
		result.Gamma = g.Gamma
		result.Theta = g.Theta
		result.Vega = g.Vega
		result.Rho = g.Rho
	}
	return result, nil
}

// GetMaxPain calls the Python Max Pain calculation.
func (c *Client) GetMaxPain(ctx context.Context, underlying string, chain []*marketdatav1.OptionChainExpiry) (float64, string, error) {
	resp, err := c.svc.GetMaxPain(ctx, &analysisv1.MaxPainRequest{
		Underlying: underlying,
		Chain:      chain,
	}, defaultCallOpts...)
	if err != nil {
		return 0, "", fmt.Errorf("GetMaxPain: %w", err)
	}
	return resp.MaxPainPrice, resp.Expiration, nil
}

// AnalyzeStock runs the full analysis pipeline on the Python engine.
func (c *Client) AnalyzeStock(ctx context.Context, req *analysisv1.AnalyzeStockRequest) (*analysisv1.AnalyzeStockResponse, error) {
	resp, err := c.svc.AnalyzeStock(ctx, req, defaultCallOpts...)
	if err != nil {
		return nil, fmt.Errorf("AnalyzeStock: %w", err)
	}
	return resp, nil
}

// BatchQuickAnalysis runs quick analysis on multiple stocks for the dashboard.
func (c *Client) BatchQuickAnalysis(ctx context.Context, req *analysisv1.BatchQuickAnalysisRequest) (*analysisv1.BatchQuickAnalysisResponse, error) {
	resp, err := c.svc.BatchQuickAnalysis(ctx, req, defaultCallOpts...)
	if err != nil {
		return nil, fmt.Errorf("BatchQuickAnalysis: %w", err)
	}
	return resp, nil
}
