package random

import (
	"fmt"
	"math/rand"
)

func ShortID() string {
	return fmt.Sprintf("%04x", rand.Intn(65536))
}
