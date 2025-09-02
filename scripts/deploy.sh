#!/bin/bash

cd ../proxy-worker
make deploy

cd ../check-worker
make deploy

cd ../admin-worker
make deploy

cd ../cron-worker
make deploy