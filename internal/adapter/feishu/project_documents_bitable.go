package feishu

// ProjectBitableAPI reuses the selected gateway broker and the existing generic
// Bitable adapter. It does not create a separate credential or Cron data model.
func (g *LiveGateway) ProjectBitableAPI() BitableAPI {
	if g == nil || g.broker == nil {
		return nil
	}
	return &liveBitableAPI{client: g.broker.sdkClient, broker: g.broker}
}
