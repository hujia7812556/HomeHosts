package config

type Config struct {
    SSIDs []string `yaml:"ssids"`
    Hosts []string `yaml:"hosts"`
}
