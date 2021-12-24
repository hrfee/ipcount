### ipcount

Very simple user counter. The web server makes a request to `ipcount:8000/add?ip=<ip>` per request to it, and the ip is hashed and stored. GETting `ipcount:8000/count` will return a count of ips that were active within your set time period (`days, hours, minutes in config.ini`) as well as deleting any inactive ones from the database. Since there's no scheduled cleanup, i'd reccomend hitting `/count` regularly to avoid slow requests. If a GeoIP2/GeoLite2 Database is provided, countries can also be logged and a summary of where users are from can be accessed at `/countries`.

#### use

either run on the same system as your webserver with a firewall preventing outside access, or set some authentication up yourself.

stick this in a config.ini file:
```ini
; secret for hashing ips. generate with (openssl rand -hex 45) or something similar. If this changes, you're database might briefly have duplicate entries.
secret = your secret
; days hours and minutes after which a user will be considered inactive. Large time periods will result in a larger db and slower /count requests.
days = 1
hours = 0
minutes = 0
; port to run on (default 8000)
port = 

; If you want to store country for each ip, provide a GeoLite2/GeoIP2 database file path here.
geoip2_db = 
```

run with `ipcount <path to config file> <path to db file>`.

if using the included dockerfile,

```sh
$ docker create --name ipcount --restart always -p 8000:8000 -v /path/to/config.ini:/config.ini:ro -v /path/to/data:/data ipcount
```
