/**
 * Tests for the IAM policy editor logic (src/services/iamPolicyEditor.ts).
 *
 * Pure functions — no DOM, no network. They cover the visual -> JSON
 * generation, the JSON -> visual parse, and the unsupported-feature
 * cases where the JSON must be kept as-is with a warning naming the
 * feature, never silently rewritten.
 */
import { describe, expect, it } from 'vitest'
import {
  SAMPLE_VISUAL_STATEMENTS,
  blankStatement,
  documentToVisual,
  visualToDocument,
  visualToJson,
  visualValidationErrors,
} from './iamPolicyEditor.ts'

describe('visualToDocument', () => {
  it('generates the sample policy document from the visual statements', () => {
    expect(visualToDocument(SAMPLE_VISUAL_STATEMENTS)).toEqual({
      Version: '2012-10-17',
      Statement: [
        { Effect: 'Allow', Action: 's3:ListBuckets', Resource: '*' },
        { Effect: 'Deny', Action: 's3:DeleteBucket', Resource: '*' },
      ],
    })
  })

  it('collapses a single action and resource to strings, keeps several as lists', () => {
    const doc = visualToDocument([
      {
        effect: 'Allow', service: 's3',
        actions: ['s3:GetObject', 's3:PutObject'], customAction: 's3:*',
        resources: 'arn:cloudforge:s3:::my-bucket, arn:cloudforge:s3:::my-bucket/*', username: '',
      },
    ])
    expect(doc.Statement).toEqual([{
      Effect: 'Allow',
      Action: ['s3:GetObject', 's3:PutObject', 's3:*'],
      Resource: ['arn:cloudforge:s3:::my-bucket', 'arn:cloudforge:s3:::my-bucket/*'],
    }])
  })

  it('adds a StringEquals condition on aws:username only when a username is set', () => {
    const withCondition = visualToDocument([{ ...blankStatement(), actions: ['s3:ListBuckets'], username: 'ada' }])
    expect(withCondition.Statement).toEqual([{
      Effect: 'Allow', Action: 's3:ListBuckets', Resource: '*',
      Condition: { StringEquals: { 'aws:username': 'ada' } },
    }])
    const without = visualToDocument([{ ...blankStatement(), actions: ['s3:ListBuckets'], username: '' }])
    expect(without.Statement).toEqual([{ Effect: 'Allow', Action: 's3:ListBuckets', Resource: '*' }])
  })

  it('round-trips through JSON text', () => {
    const json = visualToJson(SAMPLE_VISUAL_STATEMENTS)
    expect(JSON.parse(json)).toEqual(visualToDocument(SAMPLE_VISUAL_STATEMENTS))
    expect(documentToVisual(json).statements).toEqual(SAMPLE_VISUAL_STATEMENTS)
  })
})

describe('documentToVisual', () => {
  it('parses the sample JSON policy into visual statements', () => {
    const sample = `{
      "Version": "2012-10-17",
      "Statement": [
        { "Effect": "Allow", "Action": "s3:ListBuckets", "Resource": "*" },
        { "Effect": "Deny", "Action": "s3:DeleteBucket", "Resource": "*" }
      ]
    }`
    expect(documentToVisual(sample)).toEqual({ statements: SAMPLE_VISUAL_STATEMENTS, unsupported: [], jsonError: null })
  })

  it('parses a StringEquals aws:username condition into the username field', () => {
    const result = documentToVisual({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', Action: 'dynamodb:GetItem', Resource: '*', Condition: { StringEquals: { 'aws:username': 'ada' } } }],
    })
    expect(result.unsupported).toEqual([])
    expect(result.statements).toEqual([
      { effect: 'Allow', service: 'dynamodb', actions: ['dynamodb:GetItem'], customAction: '', resources: '*', username: 'ada' },
    ])
  })

  it('keeps an unlisted real action instead of dropping it', () => {
    const result = documentToVisual({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', Action: 's3:GetBucketAcl', Resource: '*' }],
    })
    expect(result.unsupported).toEqual([])
    expect(result.statements?.[0].actions).toEqual(['s3:GetBucketAcl'])
  })

  it('reports invalid JSON as a json error, not an unsupported feature', () => {
    const result = documentToVisual('{ not json')
    expect(result.statements).toBeNull()
    expect(result.unsupported).toEqual([])
    expect(result.jsonError).not.toBeNull()
  })

  it('names policy variables as unsupported and keeps the JSON', () => {
    const result = documentToVisual({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', Action: 's3:GetObject', Resource: 'arn:cloudforge:s3:::${aws:username}/*' }],
    })
    expect(result.statements).toBeNull()
    expect(result.unsupported.join(' ')).toMatch(/\$\{\.\.\.\}/)
  })

  it('names NotAction, Principal and other condition operators as unsupported', () => {
    const notAction = documentToVisual({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', NotAction: 's3:*', Resource: '*' }],
    })
    expect(notAction.statements).toBeNull()
    expect(notAction.unsupported.join(' ')).toMatch(/NotAction/)

    const principal = documentToVisual({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', Principal: { AWS: 'arn:cloudforge:iam::local:user/alice' }, Action: 'sts:AssumeRole', Resource: '*' }],
    })
    expect(principal.statements).toBeNull()
    expect(principal.unsupported.join(' ')).toMatch(/Principal/)

    const otherCondition = documentToVisual({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', Action: 's3:GetObject', Resource: '*', Condition: { StringLike: { 'aws:username': 'a*' } } }],
    })
    expect(otherCondition.statements).toBeNull()
    expect(otherCondition.unsupported.join(' ')).toMatch(/StringLike/)
  })

  it('refuses a statement mixing services, naming both services', () => {
    const result = documentToVisual({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', Action: ['s3:GetObject', 'ec2:DescribeInstances'], Resource: '*' }],
    })
    expect(result.statements).toBeNull()
    expect(result.unsupported.join(' ')).toMatch(/s3.*ec2|ec2.*s3/)
  })

  it('refuses an unknown Version', () => {
    const result = documentToVisual({ Version: '2020-01-01', Statement: [{ Effect: 'Allow', Action: 's3:ListBuckets', Resource: '*' }] })
    expect(result.statements).toBeNull()
    expect(result.unsupported.join(' ')).toMatch(/Version/)
  })
})

describe('visualValidationErrors', () => {
  it('requires at least one statement and one action per statement', () => {
    expect(visualValidationErrors([])).toEqual(['Add at least one statement.'])
    expect(visualValidationErrors([blankStatement()])).toEqual(['Statement 1: pick at least one action or type a custom action.'])
    expect(visualValidationErrors([{ ...blankStatement(), actions: ['s3:ListBuckets'] }])).toEqual([])
  })
})
