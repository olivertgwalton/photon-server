package main

import (
	"gorm.io/gen"

	"github.com/olivertgwalton/photon-server/internal/store/model"
)

func main() {
	g := gen.NewGenerator(gen.Config{OutPath: "query", Mode: gen.WithQueryInterface})
	g.ApplyBasic(model.All()...)
	g.Execute()
}
