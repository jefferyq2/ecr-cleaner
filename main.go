package main

import (
	"flag"
	"fmt"
	"log"
	"sort"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"regexp"

	"strings"
)

// TagFiltering type for image tag filtering
type TagFiltering struct {
	regExp string
	action string
}

func main() {
	var (
		amountToKeep  = flag.Int("keep", 100, "amount of images / repo you want to keep")
		awsRegion     = flag.String("aws.region", "eu-central-1", "AWS region")
		repoToProcess = flag.String("repo", "", "repository you want to process, empty if you want all")
		dryRun        = flag.Bool("dry-run", false, "run the code without actual deleting")
		tagRE         = flag.String("tag-regexp", "", "regexp for filtering images")
		postFilter    = flag.String("post-filter-action", "delete", "images with regexp tags can be deleted or saved.")
		err           error
		tagFilter     TagFiltering
	)

	flag.Parse()
	_, err = regexp.Compile(*tagRE)
	if err != nil {
		log.Println("Incorrect RegExp")
		log.Fatal(err)
	}
	tagFilter.regExp = *tagRE

	tagFilter.action = strings.ToLower(*postFilter)
	if !strings.Contains("deletesave", tagFilter.action) {
		log.Fatalf("Incorrect value %v . only delete and save are supported", tagFilter.action)
	}

	ecrCli := ecr.New(session.New(), aws.NewConfig().WithRegion(*awsRegion))
	repos := []string{*repoToProcess}
	if *repoToProcess == "" {
		repos, err = getAllRepoNames(ecrCli)
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("Repositories to process: %v", repos)

	for _, repoName := range repos {
		images, err := getImages(ecrCli, repoName)
		if err != nil {
			log.Fatalf("Could not retrieve images for repo %v: %v", repoName, err)
		}
		log.Printf("Number of images in %v: %v", repoName, len(images))

		err = cleanupImages(ecrCli, repoName, images, *dryRun, *amountToKeep, tagFilter)
		if err != nil {
			log.Fatalf("Could not clean up images for repo %v: %v", repoName, err)
		}
	}
}

func cleanupImages(ecrCli *ecr.ECR, repoName string, images []*ecr.ImageDetail, dryRun bool, amountToKeep int, tagFilter TagFiltering) error {
	var deleteImageIDs []*ecr.ImageDetail

	imagesNoTag, imagesWithTag := separateHavingTag(images)

	//filter image tags with regexp
	imagesWithRETag, imagesWoRETag := filterTags(imagesWithTag, tagFilter.regExp)

	//based on post-filter-action filtered images will be saved or deleted
	if strings.Contains(tagFilter.action, "delete") {
		imagesWithTag = imagesWithRETag
	} else {
		imagesWithTag = imagesWoRETag
	}

	//delete all images without tag
	deleteImageIDs = append(deleteImageIDs, imagesNoTag...)

	//delete images with tag so that `amountToKeep` remain
	sort.Sort(byTime(imagesWithTag))
	imagesWithTagToRemove := imagesToRemove(imagesWithTag, amountToKeep)
	deleteImageIDs = append(deleteImageIDs, imagesWithTagToRemove...)

	log.Printf("number of images to delete: %v", len(deleteImageIDs))

	if dryRun {
		log.Print("dry run ...")
		log.Printf("images to delete: %v", deleteImageIDs)
		return nil
	}

	if len(deleteImageIDs) <= 0 {
		log.Printf("nothing to do so skip %v", repoName)
		return nil
	}

	i := 0
	for i = 0; i < int(len(deleteImageIDs)/100); i++ {
		err := deleteImages(ecrCli, repoName, deleteImageIDs[i*100:(i+1)*100])

		if err != nil {
			return fmt.Errorf("deleting images in repo %v: %v", repoName, err)
		}
	}

	err := deleteImages(ecrCli, repoName, deleteImageIDs[i*100:])

	if err != nil {
		return fmt.Errorf("deleting images in repo %v: %v", repoName, err)
	}

	log.Printf("deleted %v images in repo %v", len(deleteImageIDs), repoName)

	return nil
}

func filterTags(images []*ecr.ImageDetail, tagRE string) (imagesWithRETags []*ecr.ImageDetail, imgsWoRETags []*ecr.ImageDetail) {
	if tagRE == "" {
		// skipping  check if original regexp was empty
		return images, imgsWoRETags
	}

	for _, image := range images {
		for _, tag := range image.ImageTags {
			matched, err := regexp.MatchString(tagRE, *tag)
			if err != nil {
				log.Fatal(err)
			}
			if matched {
				imagesWithRETags = append(imagesWithRETags, image)
				break
			}
		}
		if (len(imagesWithRETags) == 0) || (imagesWithRETags[len(imagesWithRETags)-1].ImageDigest != image.ImageDigest) {
			imgsWoRETags = append(imgsWoRETags, image)
		}
	}
	return imagesWithRETags, imgsWoRETags
}

func buildImageIdentifier(images []*ecr.ImageDetail) []*ecr.ImageIdentifier {
	var imageIdentifiers = []*ecr.ImageIdentifier{}

	for _, image := range images {
		imageIdentifiers = append(imageIdentifiers, &ecr.ImageIdentifier{
			ImageDigest: image.ImageDigest,
		})
	}
	return imageIdentifiers
}

func deleteImages(ecrCli *ecr.ECR, repoName string, images []*ecr.ImageDetail) error {
	imageIdentifiers := buildImageIdentifier(images)

	_, err := ecrCli.BatchDeleteImage(&ecr.BatchDeleteImageInput{
		RepositoryName: aws.String(repoName),
		ImageIds:       imageIdentifiers,
	})
	if err != nil {
		return fmt.Errorf("deleting images in repo %v: %v", repoName, err)
	}

	return nil
}

type byTime []*ecr.ImageDetail

func (p byTime) Len() int           { return len(p) }
func (p byTime) Less(i, j int) bool { return p[i].ImagePushedAt.Unix() < p[j].ImagePushedAt.Unix() }
func (p byTime) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func imagesToRemove(images []*ecr.ImageDetail, amountToKeep int) []*ecr.ImageDetail {
	if len(images) < amountToKeep {
		return []*ecr.ImageDetail{}
	}
	return images[0 : len(images)-amountToKeep]
}

func separateHavingTag(images []*ecr.ImageDetail) (imagesWithoutTag []*ecr.ImageDetail, imagesWithTag []*ecr.ImageDetail) {
	for _, image := range images {
		if len(image.ImageTags) == 0 {
			imagesWithoutTag = append(imagesWithoutTag, image)
		} else {
			imagesWithTag = append(imagesWithTag, image)
		}
	}

	return imagesWithoutTag, imagesWithTag
}

func getImages(ecrCli *ecr.ECR, repoName string) ([]*ecr.ImageDetail, error) {
	var (
		token    *string
		imageIDs = []*ecr.ImageDetail{}
	)
	for {
		params := &ecr.DescribeImagesInput{
			RepositoryName: aws.String(repoName),
			NextToken:      token,
		}
		resp, err := ecrCli.DescribeImages(params)
		if err != nil {
			return nil, fmt.Errorf("getting %v image details: %v", repoName, err)
		}
		imageIDs = append(imageIDs, resp.ImageDetails...)
		if token = resp.NextToken; token == nil {
			break
		}
	}
	return imageIDs, nil
}

func getAllRepoNames(ecrCli *ecr.ECR) ([]string, error) {
	resp, err := ecrCli.DescribeRepositories(&ecr.DescribeRepositoriesInput{})
	if err != nil {
		return []string{}, fmt.Errorf("getting ecr repos: %v", err)
	}

	repos := make([]string, 0, len(resp.Repositories))
	for _, repo := range resp.Repositories {
		repos = append(repos, *repo.RepositoryName)
	}
	return repos, nil
}
