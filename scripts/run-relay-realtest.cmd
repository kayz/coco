@echo off
cd /d C:\git\coco
coco.exe relay --platform wecom --user-id kermi --token coco-p4ASKK3aXbxOejJLMk7HI90r3XWxqmf9 --server wss://www.greatquant.com/ws --webhook https://www.greatquant.com/webhook --log info 1>> realtest-relay.out.log 2>> realtest-relay.err.log
