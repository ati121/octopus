package op

import "github.com/bestruirui/octopus/internal/model"

var resetRelayBalancerStateForChannel func(int)
var configureRelayCircuitBreaker func(bool)

// RegisterRelayCircuitBreakerToggle 接入熔断开关，避免设置层依赖 relay 包。
func RegisterRelayCircuitBreakerToggle(fn func(bool)) {
	configureRelayCircuitBreaker = fn
	syncCircuitBreakerSetting()
}

func syncCircuitBreakerSetting() {
	if configureRelayCircuitBreaker != nil {
		enabled, _ := SettingGetBool(model.SettingKeyCircuitBreakerEnabled)
		configureRelayCircuitBreaker(enabled)
	}
}

func RegisterRelayBalancerStateReset(fn func(int)) {
	resetRelayBalancerStateForChannel = fn
}

func resetBalancerStateForChannel(channelID int) {
	if resetRelayBalancerStateForChannel != nil {
		resetRelayBalancerStateForChannel(channelID)
	}
}

func resetBalancerStateForChannels(channelIDs ...int) {
	if resetRelayBalancerStateForChannel == nil || len(channelIDs) == 0 {
		return
	}
	seen := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID == 0 {
			continue
		}
		if _, ok := seen[channelID]; ok {
			continue
		}
		seen[channelID] = struct{}{}
		resetRelayBalancerStateForChannel(channelID)
	}
}
