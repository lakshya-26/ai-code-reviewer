func GetUser(id string) (*User, error) {
    query := "SELECT * FROM users WHERE id = " + id  // SQL injection
    return db.Query(query)
}