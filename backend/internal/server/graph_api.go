package server

import "ikhnos/internal/landscape"

func (a *Admin) graphAPI() landscape.GraphAPI {
	g, _ := a.C.Store.(landscape.GraphAPI)
	return g
}
