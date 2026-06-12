package testbugs

import (
	"database/sql"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// ── SECURITY BUGS ─────────────────────────────────────────────────────────────

// Bug 1: SQL Injection — user input directly in query string
func GetUser(db *sql.DB, userID string) {
	query := "SELECT * FROM users WHERE id = " + userID
	rows, _ := db.Query(query)
	fmt.Println(rows)
}

// Bug 2: Command injection — user input passed directly to shell
func RunReport(userInput string) {
	cmd := exec.Command("sh", "-c", "generate-report "+userInput)
	cmd.Run()
}

// Bug 3: Hardcoded credentials
func ConnectDB() *sql.DB {
	dsn := "postgres://admin:SuperSecret123@prod-db.internal:5432/users"
	db, _ := sql.Open("postgres", dsn)
	return db
}

// Bug 4: Insecure random — math/rand is not cryptographically secure
func GenerateToken() string {
	return fmt.Sprintf("%d", rand.Int())
}

// Bug 5: Path traversal — user controls file path
func ReadFile(filename string) string {
	data, _ := os.ReadFile("/var/data/" + filename)
	return string(data)
}

// ── RESOURCE LEAKS ────────────────────────────────────────────────────────────

// Bug 6: HTTP response body never closed
func FetchData(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		return ""
	}
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// Bug 7: DB rows never closed
func ListUsers(db *sql.DB) []string {
	rows, err := db.Query("SELECT name FROM users")
	if err != nil {
		return nil
	}
	var names []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		names = append(names, name)
	}
	return names
}

// Bug 8: File handle never closed
func WriteLog(msg string) {
	f, err := os.Open("app.log")
	if err != nil {
		return
	}
	f.WriteString(msg)
}

// ── LOGIC / BUG ───────────────────────────────────────────────────────────────

// Bug 9: Off-by-one — last element never processed
func SumAll(nums []int) int {
	total := 0
	for i := 0; i < len(nums)-1; i++ {
		total += nums[i]
	}
	return total
}

// Bug 10: Nil dereference — pointer used without nil check
func GetName(user *struct{ Name string }) string {
	return user.Name
}

// Bug 11: Shadowed error — err from inner scope shadows outer
func ProcessItems(db *sql.DB) error {
	_, err := db.Query("SELECT 1")
	if err != nil {
		return err
	}
	if true {
		err := fmt.Errorf("inner error")
		fmt.Println(err)
	}
	return nil
}

// ── PERFORMANCE ───────────────────────────────────────────────────────────────

// Bug 12: N+1 query — DB call inside a loop
func GetAllUserOrders(db *sql.DB, userIDs []int) {
	for _, id := range userIDs {
		rows, _ := db.Query(fmt.Sprintf("SELECT * FROM orders WHERE user_id = %d", id))
		fmt.Println(rows)
	}
}

// Bug 13: String concatenation in a loop — O(n²) allocations
func BuildCSV(items []string) string {
	result := ""
	for _, item := range items {
		result = result + item + ","
	}
	return result
}

// Bug 14: Unbounded query — no LIMIT
func GetAllLogs(db *sql.DB) {
	rows, _ := db.Query("SELECT * FROM logs")
	fmt.Println(rows)
}

// ── CODE SMELL ────────────────────────────────────────────────────────────────

// Bug 15: Magic numbers with no explanation
func CalculateFee(amount float64) float64 {
	if amount > 10000 {
		return amount * 0.035
	} else if amount > 5000 {
		return amount * 0.05
	} else if amount > 1000 {
		return amount * 0.075
	}
	return amount * 0.1
}

// Bug 16: Function doing too many things — violates single responsibility
func HandleRequest(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("id")
	query := "SELECT * FROM users WHERE id = " + userID
	rows, err := db.Query(query)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}
	var names []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		names = append(names, name)
	}
	result := ""
	for _, n := range names {
		result = result + n + "\n"
	}
	fmt.Fprintf(w, result)
}

// Bug 17: Misleading function name — does the opposite of what it says
func IsValid(input string) bool {
	return strings.Contains(input, "<script>")
}

// Bug 18: Unchecked type assertion — will panic on wrong type
func ProcessValue(v interface{}) string {
	return v.(string)
}
