# Release _notes_

This release adds **three** features and fixes a [nasty bug](https://example.com/bugs/42 "Bug 42"). Whitespace is collapsed.

## Highlights

* Faster `parse()`
* New nodes
  1. XML
  2. HTML
* ~~Old API~~ removed

### Example

```go
func main() {
	fmt.Println("*not emphasis*")
}
```

> Quoted text.
>
> Second line of the quote.

Stars \* and \_underscores\_ and \[brackets\] are escaped.  
After a break.

\# not a heading

---

| Node | Status |
| --- | --- |
| data.xml | done |
| data.html | a \| b |

![Logo](https://example.com/logo.png)