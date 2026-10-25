package store_dynamodb

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// RoleCode is an external ID Pennsieve issued with a role configuration
// (GET /role-policy): to whom, for which role, until when, and whether a
// registration has used it. Registration accepts an external ID only as one
// of these, issued to the user registering, so a code seen elsewhere (in the
// role's trust policy) can't register the role for someone else.
type RoleCode struct {
	ExternalId string `dynamodbav:"externalId"`
	UserId     string `dynamodbav:"userId"`
	RoleName   string `dynamodbav:"roleName"`
	IssuedAt   int64  `dynamodbav:"issuedAt"`  // epoch seconds
	ExpiresAt  int64  `dynamodbav:"expiresAt"` // epoch seconds; the table's TTL
	Used       bool   `dynamodbav:"used"`
}

// ErrRoleCodeUnavailable: the code isn't one issued to that user for that
// role, or it has expired or been used.
var ErrRoleCodeUnavailable = errors.New("role code not available")

// RoleCodeStore keeps the external IDs Pennsieve has issued.
type RoleCodeStore interface {
	// Issue records a newly issued code; an existing code is never replaced.
	Issue(ctx context.Context, code RoleCode) error
	// Get reads a code; found is false when there's none (or DynamoDB's TTL
	// has removed it).
	Get(ctx context.Context, externalId string) (code RoleCode, found bool, err error)
	// MarkUsed uses up the code for userId's registration of roleName, in one
	// conditional write: ErrRoleCodeUnavailable unless it was issued to them,
	// for that role, hasn't expired at now (epoch seconds) and is unused. Two
	// registrations racing on one code can't both succeed.
	MarkUsed(ctx context.Context, externalId, userId, roleName string, now int64) error
	// Release makes a used code usable again, for a registration that used it
	// and then failed to store the account.
	Release(ctx context.Context, externalId string) error
}

type RoleCodeDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewRoleCodeStore(db *dynamodb.Client, tableName string) RoleCodeStore {
	return &RoleCodeDatabaseStore{DB: db, TableName: tableName}
}

func (s *RoleCodeDatabaseStore) key(externalId string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"externalId": &types.AttributeValueMemberS{Value: externalId}}
}

func (s *RoleCodeDatabaseStore) Issue(ctx context.Context, code RoleCode) error {
	item, err := attributevalue.MarshalMap(code)
	if err != nil {
		return fmt.Errorf("marshalling role code: %w", err)
	}
	if _, err := s.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.TableName),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(externalId)"),
	}); err != nil {
		return fmt.Errorf("issuing role code: %w", err)
	}
	return nil
}

func (s *RoleCodeDatabaseStore) Get(ctx context.Context, externalId string) (RoleCode, bool, error) {
	resp, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.TableName),
		Key:            s.key(externalId),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return RoleCode{}, false, fmt.Errorf("getting role code: %w", err)
	}
	if len(resp.Item) == 0 {
		return RoleCode{}, false, nil
	}
	var code RoleCode
	if err := attributevalue.UnmarshalMap(resp.Item, &code); err != nil {
		return RoleCode{}, false, fmt.Errorf("unmarshalling role code: %w", err)
	}
	return code, true, nil
}

func (s *RoleCodeDatabaseStore) MarkUsed(ctx context.Context, externalId, userId, roleName string, now int64) error {
	_, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:        aws.String(s.TableName),
		Key:              s.key(externalId),
		UpdateExpression: aws.String("SET used = :true"),
		ConditionExpression: aws.String(
			"attribute_exists(externalId) AND userId = :user AND roleName = :role AND expiresAt > :now AND used = :false"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":true":  &types.AttributeValueMemberBOOL{Value: true},
			":false": &types.AttributeValueMemberBOOL{Value: false},
			":user":  &types.AttributeValueMemberS{Value: userId},
			":role":  &types.AttributeValueMemberS{Value: roleName},
			":now":   &types.AttributeValueMemberN{Value: strconv.FormatInt(now, 10)},
		},
	})
	var failed *types.ConditionalCheckFailedException
	if errors.As(err, &failed) {
		return ErrRoleCodeUnavailable
	}
	if err != nil {
		return fmt.Errorf("using role code: %w", err)
	}
	return nil
}

func (s *RoleCodeDatabaseStore) Release(ctx context.Context, externalId string) error {
	if _, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(s.TableName),
		Key:                       s.key(externalId),
		UpdateExpression:          aws.String("SET used = :false"),
		ConditionExpression:       aws.String("attribute_exists(externalId)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":false": &types.AttributeValueMemberBOOL{Value: false}},
	}); err != nil {
		return fmt.Errorf("releasing role code: %w", err)
	}
	return nil
}
