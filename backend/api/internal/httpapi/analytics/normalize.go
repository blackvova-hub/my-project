package analytics

// Split a Bybit one-way reversal into the hedge-compatible closing and opening
// inventory lanes. Every original execution and its apportioned fee is retained.
func splitBybitReversal(f Fill) []Fill {
	if f.ClosedQuantity == nil || *f.ClosedQuantity <= 0 || *f.ClosedQuantity >= f.Quantity {
		return []Fill{f}
	}
	closeFill, openFill := f, f
	closeFill.ID = f.ID + ":0close"
	closeFill.Quantity = *f.ClosedQuantity
	closeFill.Fee = f.Fee * closeFill.Quantity / f.Quantity
	openFill.ID = f.ID + ":1open"
	openFill.Quantity = f.Quantity - closeFill.Quantity
	openFill.Fee = f.Fee - closeFill.Fee
	zero := 0.0
	openFill.ClosedQuantity = &zero
	if openFill.Side == "BUY" {
		openFill.PositionSide = "LONG"
	} else {
		openFill.PositionSide = "SHORT"
	}
	return []Fill{closeFill, openFill}
}
