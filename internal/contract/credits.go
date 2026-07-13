package contract

// Credit conversion: the gateway plan grants 2,500 credits per $25 of budget,
// i.e. 100 credits per dollar (1 credit = $0.01). Every credit figure shown to
// the user must go through these helpers so the rate lives in exactly one place.
const (
	PlanCredits   = 2500.0
	PlanBudgetUSD = 25.0
	CreditsPerUSD = PlanCredits / PlanBudgetUSD
)

// USDToCredits converts a gateway USD amount into display credits.
func USDToCredits(usd float64) float64 { return usd * CreditsPerUSD }

// CreditsToUSD converts credits back into gateway USD.
func CreditsToUSD(credits float64) float64 { return credits / CreditsPerUSD }
