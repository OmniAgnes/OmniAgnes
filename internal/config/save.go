package config

// SaveProviderConfig 将单个 provider 配置合并进配置文件：
// 同 type+model 原地更新，否则追加；并把它设为默认 provider，下次启动直接生效。
func SaveProviderConfig(path, typ, model, baseURL, apiKey string) error {
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	for i := range cfg.Providers {
		if cfg.Providers[i].Type == typ && cfg.Providers[i].Model == model {
			cfg.Providers[i].BaseURL = baseURL
			cfg.Providers[i].APIKey = apiKey
			cfg.Providers[i].Default = true
			cfg.Provider = model
			return cfg.Save(path)
		}
	}
	cfg.Providers = append(cfg.Providers, ProviderConfig{
		Type: typ, Model: model, BaseURL: baseURL, APIKey: apiKey, Default: true,
	})
	cfg.Provider = model
	return cfg.Save(path)
}
