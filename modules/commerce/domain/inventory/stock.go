package inventory

import "example.com/ecommerce/commerce/domain/fault"

// Deduct is a pure rule. The storage adapter supplies atomic concurrency control.
func Deduct(available, quantity int) (int, error) {
	if quantity < 1 {
		return available, fault.Invalid
	}
	if available < quantity {
		return available, fault.OutOfStock
	}
	return available - quantity, nil
}
