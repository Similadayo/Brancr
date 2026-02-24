package tenantapi

import (
	"net/http"
	"strings"
)

// NigerianStates contains all 36 states plus FCT
var NigerianStates = []string{
	"Abia", "Adamawa", "Akwa Ibom", "Anambra", "Bauchi", "Bayelsa", "Benue", "Borno",
	"Cross River", "Delta", "Ebonyi", "Edo", "Ekiti", "Enugu", "Federal Capital Territory",
	"Gombe", "Imo", "Jigawa", "Kaduna", "Kano", "Katsina", "Kebbi", "Kogi", "Kwara",
	"Lagos", "Nassarawa", "Niger", "Ogun", "Ondo", "Osun", "Oyo", "Plateau", "Rivers",
	"Sokoto", "Taraba", "Yobe", "Zamfara",
}

// NigerianLGAs maps states to their LGAs
var NigerianLGAs = map[string][]string{
	"Lagos": {
		"Agege", "Ajeromi-Ifelodun", "Alimosho", "Amuwo-Odofin", "Apapa", "Badagry",
		"Epe", "Eti-Osa", "Ibeju-Lekki", "Ifako-Ijaiye", "Ikeja", "Ikorodu", "Kosofe",
		"Lagos Island", "Lagos Mainland", "Mushin", "Ojo", "Oshodi-Isolo", "Shomolu", "Surulere",
	},
	"Federal Capital Territory": {
		"Abaji", "Bwari", "Gwagwalada", "Kuje", "Kwali", "Municipal",
	},
	"Ogun": {
		"Abeokuta North", "Abeokuta South", "Ado-Odo/Ota", "Egbado North", "Egbado South",
		"Ewekoro", "Ifo", "Ijebu East", "Ijebu North", "Ijebu North East", "Ijebu Ode",
		"Ikenne", "Imeko-Afon", "Ipokia", "Obafemi-Owode", "Odeda", "Odogbolu", "Ogun Waterside",
		"Remo North", "Sagamu",
	},
	"Oyo": {
		"Afijio", "Akinyele", "Atiba", "Atisbo", "Egbeda", "Ibadan North", "Ibadan North-East",
		"Ibadan North-West", "Ibadan South-East", "Ibadan South-West", "Ibarapa Central",
		"Ibarapa East", "Ibarapa North", "Ido", "Irepo", "Iseyin", "Itesiwaju", "Iwajowa",
		"Kajola", "Lagelu", "Ogbomosho North", "Ogbomosho South", "Ogo Oluwa", "Olorunsogo",
		"Oluyole", "Ona Ara", "Orelope", "Ori Ire", "Oyo East", "Oyo West", "Saki East",
		"Saki West", "Surulere",
	},
	"Rivers": {
		"Abua/Odual", "Ahoada East", "Ahoada West", "Akuku Toru", "Andoni", "Asari-Toru",
		"Bonny", "Degema", "Eleme", "Emohua", "Etche", "Gokana", "Ikwerre", "Khana",
		"Obio/Akpor", "Ogba/Egbema/Ndoni", "Ogu/Bolo", "Okrika", "Omuma", "Opobo/Nkoro",
		"Oyigbo", "Port Harcourt", "Tai",
	},
	"Kano": {
		"Ajingi", "Albasu", "Bagwai", "Bebeji", "Bichi", "Bunkure", "Dala", "Dambatta",
		"Dawakin Kudu", "Dawakin Tofa", "Doguwa", "Fagge", "Gabasawa", "Garko", "Garun Malam",
		"Gaya", "Gezawa", "Gwale", "Gwarzo", "Kabo", "Kano Municipal", "Karaye", "Kibiya",
		"Kiru", "Kumbotso", "Kunchi", "Kura", "Madobi", "Makoda", "Minjibir", "Nasarawa",
		"Rano", "Rimin Gado", "Rogo", "Shanono", "Sumaila", "Takai", "Tarauni", "Tofa",
		"Tsanyawa", "Tudun Wada", "Ungogo", "Warawa", "Wudil",
	},
	"Kaduna": {
		"Birnin Gwari", "Chikun", "Giwa", "Igabi", "Ikara", "Jaba", "Jema'a", "Kachia",
		"Kaduna North", "Kaduna South", "Kagarko", "Kajuru", "Kaura", "Kauru", "Kubau",
		"Kudan", "Lere", "Makarfi", "Sabon Gari", "Sanga", "Soba", "Zangon Kataf", "Zaria",
	},
	// Popular delivery areas (simplified for common use cases)
	"Abia":        {"Aba North", "Aba South", "Arochukwu", "Bende", "Ikwuano", "Isiala Ngwa North", "Isiala Ngwa South", "Isuikwuato", "Oboma Ngwa", "Ohafia", "Osisioma", "Ugwunagbo", "Ukwa East", "Ukwa West", "Umu-Nneochi", "Umuahia North", "Umuahia South"},
	"Delta":       {"Aniocha North", "Aniocha South", "Bomadi", "Burutu", "Ethiope East", "Ethiope West", "Ika North East", "Ika South", "Isoko North", "Isoko South", "Ndokwa East", "Ndokwa West", "Okpe", "Oshimili North", "Oshimili South", "Patani", "Sapele", "Udu", "Ughelli North", "Ughelli South", "Ukwuani", "Uvwie", "Warri North", "Warri South", "Warri South West"},
	"Enugu":       {"Aninri", "Awgu", "Enugu East", "Enugu North", "Enugu South", "Ezeagu", "Igbo Etiti", "Igbo Eze North", "Igbo Eze South", "Isi Uzo", "Nkanu East", "Nkanu West", "Nsukka", "Oji River", "Udenu", "Udi", "Uzo-Uwani"},
	"Anambra":     {"Aguata", "Anambra East", "Anambra West", "Anaocha", "Awka North", "Awka South", "Ayamelum", "Dunukofia", "Ekwusigo", "Idemili North", "Idemili South", "Ihiala", "Njikoka", "Nnewi North", "Nnewi South", "Ogbaru", "Onitsha North", "Onitsha South", "Orumba North", "Orumba South", "Oyi"},
	"Imo":         {"Aboh Mbaise", "Ahiazu Mbaise", "Ehime Mbano", "Ezinihitte", "Ideato North", "Ideato South", "Ihitte/Uboma", "Ikeduru", "Isiala Mbano", "Isu", "Mbaitoli", "Ngor Okpala", "Njaba", "Nkwerre", "Nwangele", "Obowo", "Oguta", "Ohaji/Egbema", "Okigwe", "Onuimo", "Orlu", "Orsu", "Oru East", "Oru West", "Owerri Municipal", "Owerri North", "Owerri West"},
	"Edo":         {"Akoko Edo", "Egor", "Esan Central", "Esan North East", "Esan South East", "Esan West", "Etsako Central", "Etsako East", "Etsako West", "Igueben", "Ikpoba-Okha", "Oredo", "Orhionmwon", "Ovia North East", "Ovia South West", "Owan East", "Owan West", "Uhunmwonde"},
	"Cross River": {"Abi", "Akamkpa", "Akpabuyo", "Bakassi", "Bekwarra", "Biase", "Boki", "Calabar Municipality", "Calabar South", "Etung", "Ikom", "Obanliku", "Obubra", "Obudu", "Odukpani", "Ogoja", "Yakurr", "Yala"},
}

// GetNigerianStates returns all Nigerian states
func (h *Handler) GetNigerianStates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"states": NigerianStates,
	})
}

// GetNigerianLGAs returns LGAs for a specific state
func (h *Handler) GetNigerianLGAs(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		http.Error(w, "state parameter required", http.StatusBadRequest)
		return
	}

	// Normalize state name for matching
	stateNorm := strings.TrimSpace(state)

	// Find matching state (case-insensitive)
	var lgas []string
	for stateName, stateLGAs := range NigerianLGAs {
		if strings.EqualFold(stateName, stateNorm) {
			lgas = stateLGAs
			break
		}
	}

	// If no LGAs found for state, return empty array
	if lgas == nil {
		lgas = []string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"state": state,
		"lgas":  lgas,
	})
}
