// Pipe in data from /countries to get a sorted list with emojis.

let stdin = process.openStdin();

let json = "";

stdin.on("data", (chunk) => json += chunk);

stdin.on("end", () => {
    let data = JSON.parse(json);
    let newData = new Map();
    for (let key in data) {
        if (key == "Unknown" || key == "Total") continue;
        const codePoints = key.toUpperCase().split('').map(char => 127397 + char.charCodeAt());
        newData.set(`${String.fromCodePoint(...codePoints)} ${key}`, data[key]);
    }
    const sorted = new Map([...newData.entries()].sort((a, b) => a[1] - b[1]));
    for (let key of sorted.keys()) {
        console.log(`${key}: ${newData.get(key)}`);
    }
    if ("Unknown" in data) {
        console.log(`Unknown: ${data["Unknown"]}`);
    }
    console.log(`Total: ${data["Total"]}`);
});
