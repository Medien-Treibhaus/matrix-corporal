package connector

import (
	"devture-matrix-corporal/corporal/matrix"
	"devture-matrix-corporal/corporal/policy"
	"devture-matrix-corporal/corporal/util"
	"fmt"

	"crypto/hmac"
	"crypto/sha1"

	"github.com/matrix-org/gomatrix"
)

// SynapseConnector is a MatrixConnector implementation for controlling a Synapse server.
// It is based on the base ApiConnector for doing whatever's possible,
// but also contains Synapse-specific API calls here.
type SynapseConnector struct {
	policyStore                       *policy.Store
	*ApiConnector
	registrationSharedSecret string
}

func NewSynapseConnector(
	policyStore *policy.Store,
	apiConnector *ApiConnector,
	registrationSharedSecret string,
) *SynapseConnector {
	return &SynapseConnector{
		policyStore:                       policyStore,
		ApiConnector:             apiConnector,
		registrationSharedSecret: registrationSharedSecret,
	}
}

func (me *SynapseConnector) DetermineCurrentState(
	ctx *AccessTokenContext,
	managedUserIds []string,
	adminUserId string,
) (*CurrentState, error) {
	client, err := me.createMatrixClientForUserId(ctx, adminUserId)
	if err != nil {
		return nil, err
	}

	// If the /admin/whois/{userId} API would let us differentiate between a user that exists and one that doesn't,
	// we could just loop over the managedUserIds, see which users exist and fetch the state then.
	//
	// Since we can't do that (yet), we're forced to loop over "all users on the server"
	// and figure things out that way. This is more inefficient, but there doesn't seem to be a better way
	// to do things now.

	var users []matrix.ApiAdminEntityUser
	err = client.MakeRequest("GET", client.BuildURL(fmt.Sprintf("/admin/users/%s", adminUserId)), nil, &users)
	if err != nil {
		return nil, err
	}
	var currentUserIds []string
	for _, user := range users {
		currentUserIds = append(currentUserIds, user.Id)
	}

	var usersState []CurrentUserState

	for _, userId := range managedUserIds {
		if !util.IsStringInArray(userId, currentUserIds) {
			// Avoid trying to fetch the state for a user that doesn't exist.
			// We'll get authentication errors.
			// And it's not like there could be any state anyway, so.. skip it.
			continue
		}

		userState, err := me.getUserStateByUserId(ctx, userId)
		if err != nil {
			return nil, err
		}
		usersState = append(usersState, *userState)
	}

	connectorState := &CurrentState{
		Users: usersState,
	}

	return connectorState, nil
}

func (me *SynapseConnector) EnsureUserAccountExists(userId string) error {
	userIdLocalPart, err := gomatrix.ExtractUserLocalpart(userId)
	if err != nil {
		return err
	}

	client, _ := gomatrix.NewClient(me.homeserverApiEndpoint, "", "")

	var nonceResponse matrix.ApiUserAccountRegisterNonceResponse
	err = matrix.ExecuteWithRateLimitRetries(me.logger, "user.register.nonce", func() error {
		// The canonical admin/register API is available at `/_synapse/admin/v1/register`.
		// What we hit below is an alias, which might stop working some time in the future.
		//
		// We can't hit the canonical URL easily, because gomatrix insists on pre-pending
		// `/_matrix/client/r0` to URLs built via `BuildURL()`.
		return client.MakeRequest(
			"GET",
			client.BuildURL("admin/register"),
			nil,
			&nonceResponse,
		)
	})
	if err != nil {
		return err
	}

	// We create users with random passwords.
	// Those passwords are never meant to be given out or used.
	//
	// Whenever we need to authenticate, we can just obtain an access token
	// thanks to shared-secret-auth, regardless of the database password.
	// (see ObtainNewAccessTokenForUserId)
	//
	// Whenever users need to log in, we intercept the /login API
	// and possibly turn the call into a request that shared-secret-auth understands
	// (see LoginInterceptor).
	//// passwordBytes, err := util.GenerateRandomBytes(64)
	//// if err != nil {
	////	return err
	//// }
	//// password := fmt.Sprintf("%x", passwordBytes)
	
	// This is to try to call AuthCredential and use it as the synapse password when creating the user
	policy := me.policyStore.Get()
	if policy == nil {
	//	return createInterceptorErrorResponse(loggingContextFields, matrix.ErrorUnknown, "Missing policy")
		return err
	}

	userPolicy := policy.GetUserPolicyByUserId(userId)
	password := userPolicy.AuthCredential
	
	// Generating the HMAC the same way that the `register_new_matrix_user` script from Matrix Synapse does it.
	mac := hmac.New(sha1.New, []byte(me.registrationSharedSecret))
	mac.Write([]byte(nonceResponse.Nonce))
	mac.Write([]byte("\x00"))
	mac.Write([]byte(userIdLocalPart))
	mac.Write([]byte("\x00"))
	mac.Write([]byte(password))
	mac.Write([]byte("\x00"))
	mac.Write([]byte("notadmin"))

	payload := matrix.ApiUserAccountRegisterRequestPayload{
		Nonce:    nonceResponse.Nonce,
		Username: userIdLocalPart,
		Password: password,
		Mac:      fmt.Sprintf("%x", mac.Sum(nil)),
		Type:     matrix.RegistrationTypeSharedSecret,
		Admin:    false,
	}

	var registerResponse matrix.ApiUserAccountRegisterResponse

	err = matrix.ExecuteWithRateLimitRetries(me.logger, "user.register.actual", func() error {
		// The canonical admin/register API is available at `/_synapse/admin/v1/register`.
		// What we hit below is an alias, which might stop working some time in the future.
		// See above for why we can't easily use it.
		return client.MakeRequest(
			"POST",
			client.BuildURL("admin/register"),
			payload,
			&registerResponse,
		)
	})

	if err != nil {
		// Swallow "user already exists" errors.
		// We don't care who created it and when. We only care that it exists.
		if matrix.IsErrorWithCode(err, matrix.ErrorUserInUse) {
			return nil
		}

		return err
	}

	// The register API creates an access token automatically.
	// We don't need it and we'd rather be nice and get rid of it, to keep things clean.
	clientForUser, _ := gomatrix.NewClient(me.homeserverApiEndpoint, userIdLocalPart, registerResponse.AccessToken)
	clientForUser.Logout()

	return nil
}
