package main

import (
	"fmt"
	"os"

	"omni/internal/tools"
)

func main() {
	env := &tools.Env{Cwd: ".", MaxOutput: 4000}
	reg := tools.NewRegistry(env)
	target := ""
	if len(os.Args) > 1 {
		target = os.Args[1]
	}
	args := map[string]any{}
	if target != "" {
		args["window"] = target
	}
	out, imgs, err := reg.ExecuteWithMedia(nil, "screenshot", args, false)
	if err != nil {
		fmt.Println("ERR:", err)
		os.Exit(1)
	}
	fmt.Println("OUT:", out)
	for i, im := range imgs {
		fmt.Printf("  [%d] %s base64=%d (~%d KB)\n", i, im.MediaType, len(im.Data), im.ByteSize()/1024)
	}
}
