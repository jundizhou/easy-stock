package appsettings

const DefaultCockpitPatrolIntervalMinutes = 30

func ValidCockpitPatrolIntervalMinutes(minutes int) bool {
	switch minutes {
	case 5, 10, 15, 30, 60:
		return true
	default:
		return false
	}
}

func NormalizeCockpitPatrolIntervalMinutes(minutes int) int {
	if !ValidCockpitPatrolIntervalMinutes(minutes) {
		return DefaultCockpitPatrolIntervalMinutes
	}
	return minutes
}
