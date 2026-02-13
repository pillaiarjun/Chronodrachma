package types

// ChronosPerCHRD defines the number of smallest units ("chronos") in 1 CHRD.
// 1 CHRD = 10^8 chronos (analogous to Bitcoin's satoshis).
const ChronosPerCHRD uint64 = 100_000_000

// Amount represents a quantity of CHRD in chronos (smallest indivisible unit).
type Amount uint64

// NewAmountFromCHRD converts whole CHRD to chronos.
func NewAmountFromCHRD(chrd uint64) Amount {
	return Amount(chrd * ChronosPerCHRD)
}

// ToCHRD returns the floating-point CHRD value (for display only, never arithmetic).
func (a Amount) ToCHRD() float64 {
	return float64(a) / float64(ChronosPerCHRD)
}

// BlockReward is the fixed reward per block: exactly 1 CHRD.
const BlockReward Amount = Amount(ChronosPerCHRD)
