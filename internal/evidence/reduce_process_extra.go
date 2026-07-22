package evidence

func ReduceNPM(input ProcessReductionInput) ObservationCard {
	if input.Adapter == "" {
		input.Adapter = "npm"
	}
	return ReduceProcess(input)
}

func ReducePytest(input ProcessReductionInput) ObservationCard {
	if input.Adapter == "" {
		input.Adapter = "pytest"
	}
	return ReduceProcess(input)
}
