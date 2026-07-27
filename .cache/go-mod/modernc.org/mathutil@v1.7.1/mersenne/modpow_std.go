//go:build riscv64 || loong64
// +build riscv64 loong64

package mersenne

import "math/big"

// ModPow returns b^Me % Mm. Run time grows quickly with 'e' and/or 'm' when b
// != 2 (then ModPow2 is used).
func ModPow(b, e, m uint32) (r *big.Int) {
	if m == 1 {
		return big.NewInt(0)
	}

	if b == 2 {
		x := ModPow2(e, m)
		r = big.NewInt(0)
		r.SetBit(r, int(x), 1)
		return
	}

	bb := big.NewInt(int64(b))
	r = big.NewInt(1)
	for ; e != 0; e-- {
		r.Mul(r, bb)
		Mod(r, r, m)
		bb.Mul(bb, bb)
		Mod(bb, bb, m)
	}
	return
}
