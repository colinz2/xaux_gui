package sound_cap

import "fmt"

func TransFFMediaDevParam(dev string, index int) string {
	return fmt.Sprintf("--dev-%s=%d", dev, index+1)
}
