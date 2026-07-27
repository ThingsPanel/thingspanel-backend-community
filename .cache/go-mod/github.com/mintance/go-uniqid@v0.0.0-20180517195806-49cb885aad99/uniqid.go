package uniqid

import (
	"math/rand"
	"time"
	"strconv"
	"math"
)

type Params struct {
	Prefix      string
	MoreEntropy bool
}

var entropy = int64(math.Floor(rand.New(rand.NewSource(time.Now().UnixNano())).Float64() * 0x75bcd15))

func New(params Params) string {

	var id string
	// Set prefix for unique id
	if params.Prefix != "" {
		id += params.Prefix
	}
	id += format(time.Now().Unix(), 8)
	// Increment global entropy value
	entropy++
	id += format(entropy, 5)
	// If we have more entropy add this
	if params.MoreEntropy == true {
		number := rand.New(rand.NewSource(time.Now().UnixNano())).Float64() * 10
		id += strconv.FormatFloat(number, 'E', -1, 64)[0: 10]
	}

	return id
}

func format(number int64, width int) string {
	hex := strconv.FormatInt(number, 16)

	if width <= len(hex) {
		// so long we split
		return hex[0:width]
	}

	for len(hex) < width {
		hex = "0" + hex
	}

	return hex
}
