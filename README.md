# ecr-cleaner

ecr-cleaner deletes old images based on the time they have been pushed.
It can clean up a specific repository as well as all repos within an aws account.

### Algorithm
1. Retrieve repo from ecr
2. Get repo images
3. Add all images without tags to deletion
4. Sort the remaining images by 'Pushed at' order
5. Add n oldest images to deletion
6. Delete images from the repository

### Installation
    go get github.com/WeltN24/ecr-cleaner

### Default values
    aws.region = eu-central-1
    dry-run = false
    keep = 100

### Examples
clean up all repos

`ecr-cleaner -aws.region=eu-west-1`

clean up my-awesome-repo

`ecr-cleaner -aws.region=eu-west-1 -repo=my-awesome-repo`

go for a dry run

`ecr-cleaner -aws.region=eu-west-1 -repo=my-awesome-repo -dry-run=true`

leave n images in repo

`ecr-cleaner -aws.region=eu-west-1 -repo=my-awesome-repo -keep=5`

**Note**: Most of the parameters could be specified without '=' sign.
But because of the usage of [parse flag](https://golang.org/pkg/flag/) it is
important to think about adding '=' signs for boolean parameters, otherwise the
parsing of the command line's options stops. [Issue hilighted here.](https://github.com/WeltN24/ecr-cleaner/issues/5)

### Deploying as lambda to aws

If you wish to clean up your repositories periodically you can do this with the help of terraform.
In the root of the repo:

1. you have to fork the repo
2. execute `make package`
3. go to into terraform folder
4. set up the needed variables
    * `cron` expects a string in [aws cron syntaxt](http://docs.aws.amazon.com/AmazonCloudWatch/latest/events/ScheduledEvents.html) (`0 3 1 * ? *` run lambda at 3am 1. of each month)
    * `aws_region` is the region in which you want to deploy the lambda
    * `repo_region` is the region in which you store your ec2 repositories
    * `repository` is the repo you want to process
    * `dry-run` (boolean) if you want to dry run
5. run terraform

If you want to keep the state, the easiest way is to create a shell script and write the remote state to s3.
Here is an example:

    #!/bin/bash
    terraform get -update
    terraform remote config \
        -backend=s3\
        -backend-config="bucket=maintaince" \
        -backend-config="key=ecr_cleaner/terraform.tfstate" \
        -backend-config="region=eu-central-1"

Execute the script: get remote state from s3 or create one and execute terraform afterwards.

### Run as Docker container

Build:

	docker build -t ecr-cleaner .

Run:

	docker run -e AWS_ACCESS_KEY_ID -e AWS_SECRET_ACCESS_KEY -it --rm ecr-cleaner -dry-run=true -aws.region=eu-west-1
