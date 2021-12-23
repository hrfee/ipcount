from random import randint
import sys
import requests

def randip() -> str:
    return f"{randint(0, 255)}.{randint(0, 255)}.{randint(0, 255)}.{randint(0, 255)}"


for _ in range(int(sys.argv[-1])):
    requests.post(f"http://localhost:8000/add?ip={randip()}")

