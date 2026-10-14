package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// stripeExecuteOp runs the PaymentIntent, Refund and Checkout operations.
// They live here to keep stripe.go under the file-size limit.
func stripeExecuteOp(ctx context.Context, operation, apiKey string, config map[string]interface{}, limit int, currency string) ([]workflow.Item, error) {
	switch operation {
	case "create_refund":
		return stripeCreateRefund(ctx, apiKey, config)
	case "get_payment_intent":
		id := strVal(config, "payment_intent_id")
		if id == "" {
			return nil, fmt.Errorf("stripe: payment_intent_id is required for get_payment_intent")
		}
		data, err := stripeGet(ctx, "/payment_intents/"+url.PathEscape(id), apiKey, nil)
		if err != nil {
			return nil, err
		}
		return []workflow.Item{workflow.NewItem(data)}, nil
	case "list_payment_intents":
		params := map[string]interface{}{"limit": strconv.Itoa(limit)}
		if customerID := strVal(config, "customer_id"); customerID != "" {
			params["customer"] = customerID
		}
		return stripeGetList(ctx, "/payment_intents", apiKey, params)
	case "create_checkout_session":
		return stripeCreateCheckoutSession(ctx, apiKey, config, currency)
	}
	return nil, fmt.Errorf("stripe: unknown operation %q", operation)
}

// stripeRefundReasons are the reason values the Refunds API accepts.
var stripeRefundReasons = map[string]bool{
	"duplicate": true, "fraudulent": true, "requested_by_customer": true,
}

// stripeCreateRefund refunds a PaymentIntent or a legacy Charge — in full,
// or partially when amount is set.
func stripeCreateRefund(ctx context.Context, apiKey string, config map[string]interface{}) ([]workflow.Item, error) {
	params := map[string]interface{}{}
	piID := strVal(config, "payment_intent_id")
	chargeID := strVal(config, "charge_id")
	switch {
	case piID != "" && chargeID != "":
		return nil, fmt.Errorf("stripe: set payment_intent_id or charge_id for create_refund, not both")
	case piID != "":
		params["payment_intent"] = piID
	case chargeID != "":
		params["charge"] = chargeID
	default:
		return nil, fmt.Errorf("stripe: payment_intent_id or charge_id is required for create_refund")
	}
	if amount := intVal(config, "amount"); amount > 0 {
		params["amount"] = strconv.Itoa(amount)
	}
	if reason := strVal(config, "reason"); reason != "" {
		if !stripeRefundReasons[reason] {
			return nil, fmt.Errorf("stripe: reason %q is invalid: use duplicate, fraudulent or requested_by_customer", reason)
		}
		params["reason"] = reason
	}
	data, err := stripePost(ctx, "/refunds", apiKey, params)
	if err != nil {
		return nil, err
	}
	return []workflow.Item{workflow.NewItem(data)}, nil
}

// stripeCreateCheckoutSession creates a hosted Checkout Session. line_items
// (a list of {price, quantity}) is required in payment and subscription
// mode; setup mode takes none but needs a currency.
func stripeCreateCheckoutSession(ctx context.Context, apiKey string, config map[string]interface{}, currency string) ([]workflow.Item, error) {
	mode := strVal(config, "mode")
	if mode == "" {
		mode = "payment"
	}
	if mode != "payment" && mode != "subscription" && mode != "setup" {
		return nil, fmt.Errorf("stripe: mode %q is invalid: use payment, subscription or setup", mode)
	}
	successURL := strVal(config, "success_url")
	if successURL == "" {
		return nil, fmt.Errorf("stripe: success_url is required for create_checkout_session")
	}
	params := map[string]interface{}{"mode": mode, "success_url": successURL}
	if cancelURL := strVal(config, "cancel_url"); cancelURL != "" {
		params["cancel_url"] = cancelURL
	}
	if customerID := strVal(config, "customer_id"); customerID != "" {
		params["customer"] = customerID
	}
	lineItems, err := stripeLineItems(config["line_items"])
	if err != nil {
		return nil, err
	}
	if mode == "setup" {
		params["currency"] = currency
	} else if len(lineItems) == 0 {
		return nil, fmt.Errorf("stripe: line_items is required for create_checkout_session in %s mode", mode)
	}
	for i, li := range lineItems {
		params[fmt.Sprintf("line_items[%d][price]", i)] = li.price
		params[fmt.Sprintf("line_items[%d][quantity]", i)] = strconv.Itoa(li.quantity)
	}
	data, err := stripePost(ctx, "/checkout/sessions", apiKey, params)
	if err != nil {
		return nil, err
	}
	return []workflow.Item{workflow.NewItem(data)}, nil
}

type stripeLineItem struct {
	price    string
	quantity int
}

// stripeLineItems parses line_items given either as a list of objects or as
// a JSON string of that list (what the schema's textarea produces).
// quantity defaults to 1.
func stripeLineItems(raw interface{}) ([]stripeLineItem, error) {
	var list []interface{}
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case []interface{}:
		list = v
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		if err := json.Unmarshal([]byte(v), &list); err != nil {
			return nil, fmt.Errorf("stripe: line_items must be a JSON list of {\"price\": ..., \"quantity\": ...}: %w", err)
		}
	default:
		return nil, fmt.Errorf("stripe: line_items must be a list of {price, quantity} objects, got %T", raw)
	}
	out := make([]stripeLineItem, 0, len(list))
	for i, entry := range list {
		m, ok := entry.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("stripe: line_items[%d] must be an object with price and quantity", i)
		}
		price := strVal(m, "price")
		if price == "" {
			return nil, fmt.Errorf("stripe: line_items[%d].price is required", i)
		}
		qty := 1
		switch q := m["quantity"].(type) {
		case nil:
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(q))
			if err != nil {
				return nil, fmt.Errorf("stripe: line_items[%d].quantity %q is not a whole number", i, q)
			}
			qty = n
		default:
			qty = numVal(q)
		}
		if qty < 1 {
			return nil, fmt.Errorf("stripe: line_items[%d].quantity must be at least 1", i)
		}
		out = append(out, stripeLineItem{price: price, quantity: qty})
	}
	return out, nil
}
