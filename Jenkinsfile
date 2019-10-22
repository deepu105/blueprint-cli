#!groovy
@Library('jenkins-pipeline-libs@master')
import com.xebialabs.pipeline.utils.Branches

pipeline {
    agent none

    options {
        buildDiscarder(logRotator(numToKeepStr: '20', artifactDaysToKeepStr: '7', artifactNumToKeepStr: '5'))
        timeout(time: 1, unit: 'HOURS')
        timestamps()
        ansiColor('xterm')
    }

    environment {
        REPOSITORY_NAME = 'xl-cli'
        DIST_SERVER_CRED = credentials('distserver')
        ON_PREM_CERT = "${env.ON_PREM_CERT}"
        ON_PREM_KEY = "${env.ON_PREM_KEY}"
        ON_PREM_K8S_API_URL = "${env.ON_PREM_K8S_API_URL}"
        NSF_SERVER_HOST = "${env.NSF_SERVER_HOST}"
    }

    stages {
        stage('Build XL CLI on Linux') {
            agent {
                node {
                    label 'xld||xlr'
                }
            }

            tools {
                jdk 'JDK 8u171'
            }

            steps {
                checkout scm
                sh "./gradlew goClean goBuild sonarqube -Dsonar.branch.name=${getBranch()} --info -x updateLicenses"
                stash name: "xl-up", includes: "build/linux-amd64/xl"
                script {
                  if (fileExists('build/version.dump') == true) {
                    currentVersion = readFile 'build/version.dump'

                    env.version = currentVersion
                  }
                }
            }
        }


        stage('Run XL UP Branch') {
            agent {
                node {
                    label 'xld||xlr||xli'
                }
            }

            when {
                expression {
                    !Branches.onMasterBranch(env.BRANCH_NAME) &&
                        githubLabelsPresent(this, ['run-xl-up-pr'])
                }
            }

            steps {
                script {
                    try {
                        sh "mkdir -p temp"
                        dir('temp') {
                            if (githubLabelsPresent(this, ['same-branch-on-xl-up-blueprint'])){
                                sh "git clone -b ${CHANGE_BRANCH} git@github.com:xebialabs/xl-up-blueprint.git || true"
                            } else {
                                sh "git clone git@github.com:xebialabs/xl-up-blueprint.git || true"
                            }
                        }
                        unstash name: 'xl-up'
                        awsConfigure = readFile "/var/lib/jenkins/.aws/credentials"
                        awsAccessKeyIdLine = awsConfigure.split("\n")[1]
                        awsSecretKeyIdLine = awsConfigure.split("\n")[2]
                        awsAccessKeyId = awsAccessKeyIdLine.split(" ")[2]
                        awsSecretKeyId = awsSecretKeyIdLine.split(" ")[2]
                        sh "curl https://dist.xebialabs.com/customer/licenses/download/v3/deployit-license.lic -u ${DIST_SERVER_CRED} -o temp/xl-up-blueprint/deployit-license.lic"
                        sh "curl https://dist.xebialabs.com/customer/licenses/download/v3/xl-release-license.lic -u ${DIST_SERVER_CRED} -o temp/xl-up-blueprint/xl-release.lic"
                        eksEndpoint = sh (script: 'aws eks describe-cluster --region eu-west-1 --name xl-up-master --query \'cluster.endpoint\' --output text', returnStdout: true).trim()
                        efsFileId = sh (script: 'aws efs describe-file-systems --region eu-west-1 --query \'FileSystems[0].FileSystemId\' --output text', returnStdout: true).trim()
                        nfsSharePath = "xebialabs-k8s"
                        runXlUpOnEks(awsAccessKeyId, awsSecretKeyId, eksEndpoint, efsFileId)
                        runXlUpOnPrem(nfsSharePath)

                    } catch (err) {
                        throw err
                    }
                }

            }
        }

    }
    post {
        success {
            script {
                if(env.BRANCH_NAME == 'master'){
                    slackSend color: "good", tokenCredentialId: "slack-token", message: "XL Cli master build *SUCCESS* - <${env.BUILD_URL}|click to open>", channel: 'team-developer-love'
                }
            }
        }
        failure {
            script {
                if(env.BRANCH_NAME == 'master'){
                    slackSend color: "danger", tokenCredentialId: "slack-token", message: "XL Cli master build *FAILED* - <${env.BUILD_URL}|click to open>", channel: 'team-developer-love'
                }
            }
        }
    }
}

def getBranch() {
    // on simple Jenkins pipeline job the BRANCH_NAME is not filled in, and we run it only on master
    return env.BRANCH_NAME ?: 'master'
}

def runXlUpOnEks(String awsAccessKeyId, String awsSecretKeyId, String eksEndpoint, String efsFileId) {
    sh "sed -ie 's@https://aws-eks.com:6443@${eksEndpoint}@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/eks-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@SOMEKEY@${awsAccessKeyId}@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/eks-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@SOMEMOREKEY@${awsSecretKeyId}@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/eks-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@test1234561@${efsFileId}@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/eks-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@test-eks-master@xl-up-master@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/eks-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@XldLic: ./deployit-license.lic@XldLic: temp/xl-up-blueprint/deployit-license.lic@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/eks-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@XlrLic: ./xl-release.lic@XlrLic: temp/xl-up-blueprint/xl-release.lic@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/eks-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@XlKeyStore: ./integration-tests/files/keystore.jceks@XlKeyStore: temp/xl-up-blueprint/integration-tests/files/keystore.jceks@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/eks-xld-xlr-mon-full.yaml"
    sh "./build/linux-amd64/xl up -a temp/xl-up-blueprint/integration-tests/test-cases/jenkins/eks-xld-xlr-mon-full.yaml -b xl-infra -l temp/xl-up-blueprint/ --undeploy --skip-prompts"
    sh "./build/linux-amd64/xl up -a temp/xl-up-blueprint/integration-tests/test-cases/jenkins/eks-xld-xlr-mon-full.yaml -b xl-infra -l temp/xl-up-blueprint/"
    sh "./build/linux-amd64/xl up -a temp/xl-up-blueprint/integration-tests/test-cases/jenkins/eks-xld-xlr-mon-full.yaml -b xl-infra -l temp/xl-up-blueprint/ --undeploy --skip-prompts"

}


def runXlUpOnPrem(String nsfSharePath) {
    sh """ if [[ ! -f "temp/xl-up-blueprint/k8sClientCert-onprem.crt" ]]; then 
        echo ${ON_PREM_CERT} >> temp/xl-up-blueprint/k8sClientCert-onprem-tmp.crt
        tr ' ' '\\n' < temp/xl-up-blueprint/k8sClientCert-onprem-tmp.crt > temp/xl-up-blueprint/k8sClientCert-onprem-tmp2.crt
        tr '%' ' ' < temp/xl-up-blueprint/k8sClientCert-onprem-tmp2.crt > temp/xl-up-blueprint/k8sClientCert-onprem.crt
        rm -f temp/xl-up-blueprint/k8sClientCert-onprem-tmp.crt | rm -f temp/xl-up-blueprint/k8sClientCert-onprem-tmp2.crt
    fi"""

    sh """ if [[ ! -f "temp/xl-up-blueprint/k8sClientCert-onprem.key" ]]; then
        echo ${ON_PREM_KEY} >> temp/xl-up-blueprint/k8sClientCert-onprem-tmp.key
        tr ' ' '\\n' < temp/xl-up-blueprint/k8sClientCert-onprem-tmp.key > temp/xl-up-blueprint/k8sClientCert-onprem-tmp2.key
        tr '%' ' ' < temp/xl-up-blueprint/k8sClientCert-onprem-tmp2.key > temp/xl-up-blueprint/k8sClientCert-onprem.key
        rm -f temp/xl-up-blueprint/k8sClientCert-onprem-tmp.key | rm -f temp/xl-up-blueprint/k8sClientCert-onprem-tmp2.key
    fi"""

    sh "sed -ie 's@https://k8s.com:6443@${ON_PREM_K8S_API_URL}@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/on-prem-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@K8sClientCertFile: ../xl-up/__test__/files/test-file@K8sClientCertFile: temp/xl-up-blueprint/k8sClientCert-onprem.crt@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/on-prem-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@K8sClientKeyFile: ../xl-up/__test__/files/test-file@K8sClientKeyFile: temp/xl-up-blueprint/k8sClientCert-onprem.key@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/on-prem-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@nfs-test.com@${NSF_SERVER_HOST}@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/on-prem-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@/xebialabs@/${nfsSharePath}@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/on-prem-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@XldLic: ./deployit-license.lic@XldLic: temp/xl-up-blueprint/deployit-license.lic@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/on-prem-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@XlrLic: ./xl-release.lic@XlrLic: temp/xl-up-blueprint/xl-release.lic@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/on-prem-xld-xlr-mon-full.yaml"
    sh "sed -ie 's@XlKeyStore: ./integration-tests/files/keystore.jceks@XlKeyStore: temp/xl-up-blueprint/integration-tests/files/keystore.jceks@g' temp/xl-up-blueprint/integration-tests/test-cases/jenkins/on-prem-xld-xlr-mon-full.yaml"
    sh "./build/linux-amd64/xl up -a temp/xl-up-blueprint/integration-tests/test-cases/jenkins/on-prem-xld-xlr-mon-full.yaml -b xl-infra -l temp/xl-up-blueprint/ --undeploy --skip-prompts"
    sh "./build/linux-amd64/xl up -a temp/xl-up-blueprint/integration-tests/test-cases/jenkins/on-prem-xld-xlr-mon-full.yaml -b xl-infra -l temp/xl-up-blueprint/"
    sh "./build/linux-amd64/xl up -a temp/xl-up-blueprint/integration-tests/test-cases/jenkins/on-prem-xld-xlr-mon-full.yaml -b xl-infra -l temp/xl-up-blueprint/ --undeploy --skip-prompts"
}
