// labelaudit independently checks two fixed-hash-selected disagreement tasks.
// These hand-transcribed constraints are a diagnostic, not a relabeling rule.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	perms := [][3]int{{1, 2, 3}, {1, 3, 2}, {2, 1, 3}, {2, 3, 1}, {3, 1, 2}, {3, 2, 1}}
	solutions := []map[string]any{}
	// Permutations give positions for each named sport, job and movie genre.
	for _, sport := range perms {
		for _, job := range perms {
			for _, movie := range perms {
				cricket, snowboard := sport[0], sport[2]
				social, mechanic := job[0], job[2]
				spy, mystery := movie[0], movie[1]
				if (snowboard == spy || snowboard == mechanic) && social > spy && cricket > mystery && social%2 != spy%2 && mechanic%2 == 0 {
					genre := "thriller"
					if mechanic == spy {
						genre = "spy"
					} else if mechanic == mystery {
						genre = "mystery"
					}
					solutions = append(solutions, map[string]any{"sport_positions": sport, "job_positions": job, "movie_positions": movie, "mechanic_genre": genre})
				}
			}
		}
	}
	// Truthfulness variables are named by location; each statement's speaker
	// truth value equals the proposition asserted. Narrative observations about
	// a friend/firetruck do not assert another named person's truth value.
	names := []string{"amusement", "ice", "mall", "cafe", "barber", "train", "observatory", "garden", "beach", "hotel", "aquarium", "school", "campground", "gallery"}
	truths := []map[string]bool{}
	for mask := 0; mask < 1<<len(names); mask++ {
		v := map[string]bool{}
		for i, k := range names {
			v[k] = mask&(1<<i) != 0
		}
		if v["amusement"] && v["mall"] == v["cafe"] && v["barber"] == !v["train"] && v["garden"] == !v["beach"] && v["garden"] == !v["cafe"] && v["observatory"] == !v["ice"] && v["hotel"] == !v["ice"] && v["aquarium"] == v["ice"] && v["ice"] == v["amusement"] && v["school"] == !v["train"] && v["ice"] == !v["campground"] && v["cafe"] == v["aquarium"] && v["gallery"] == !v["train"] && v["train"] == v["garden"] {
			truths = append(truths, v)
		}
	}
	out := map[string]any{"scope": "independent exhaustive checks of hand-transcribed constraints; published labels unchanged", "gpt:5c0d0e45-eb3f-5c6a-81ef-81fd39df82a9": solutions, "gpt:c5e2c58a-4f9a-5caf-9c27-1e219d9b6abd": truths}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stdout, string(raw))
}
