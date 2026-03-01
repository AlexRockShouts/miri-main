package mole_syn

type TopologyAnalysis struct {
	Steps []struct {
		ID      int    `json:"id"`
		Content string `json:"content"`
	} `json:"steps"`

	Bonds []struct {
		From        int    `json:"from"`
		To          int    `json:"to"`
		Type        string `json:"type"`
		Explanation string `json:"explanation"`
	} `json:"bonds"`

	TopologyScore    int `json:"topology_score"`
	BondDistribution struct {
		D float64 `json:"D"`
		R float64 `json:"R"`
		E float64 `json:"E"`
	} `json:"bond_distribution"`

	Assessment string `json:"assessment"`
}
